package daemon

import (
	"context"
	"errors"
	"fmt"
	"github.com/TicketsBot-cloud/common/collections"
	"github.com/TicketsBot-cloud/common/encryption"
	"github.com/TicketsBot-cloud/common/premium"
	"github.com/TicketsBot-cloud/database"
	"github.com/TicketsBot-cloud/gdl/rest/ratelimit"
	"github.com/TicketsBot-cloud/gdl/rest/request"
	"go.uber.org/zap"
	"time"
)

var (
	premiumCache  = make(map[uint64]bool)
	botNotInGuild = collections.NewSet[uint64]()
)

var errTokenUnavailable = errors.New("whitelabel token unavailable")

// whitelabelToken returns the token of a stored row. A guild can outlive its
// bot's row, in which case there is no credential and the premium lookup runs
// with an empty token. It never substitutes the public bot's token.
func whitelabelToken(ring *encryption.Keyring, bot database.WhitelabelBot) (string, error) {
	if bot.BotId == 0 {
		return "", nil
	}

	token, err := ring.ResolveStored(bot.BotId, bot.Token, bot.TokenCiphertext, bot.TokenKeyVersion)
	if err != nil {
		return "", fmt.Errorf("%w for bot %d: %w", errTokenUnavailable, bot.BotId, err)
	}

	return token, nil
}

func (d *Daemon) SweepAutoClose(ctx context.Context) {
	d.logger.Debug("Starting autoclose sweep")
	tickets, err := d.scan()
	if err != nil {
		d.logger.Error("Error querying database for tickets to close (autoclose)", zap.Error(err))
		return
	}

	// make sure we don't get a huge backlog due to a worker outage
	if err := d.redis.Del(context.Background(), "tickets:autoclose").Err(); err != nil {
		d.logger.Error("Error clearing autoclose Redis queue", zap.Error(err))
		return
	}

	d.logger.Debug("Closing tickets (autoclose)", zap.Int("count", len(tickets)))

	tokenFailedGuilds := make(map[uint64]struct{})

	for _, ticket := range tickets {
		if _, failed := tokenFailedGuilds[ticket.GuildId]; failed {
			continue
		}

		if notInGuild := botNotInGuild.Contains(ticket.GuildId); notInGuild {
			if err := d.db.AutoCloseExclude.Exclude(ctx, ticket.GuildId, ticket.TicketId); err != nil {
				d.logger.Error(
					"Error excluding ticket from autoclose",
					zap.Error(err),
					zap.Uint64("guild", ticket.GuildId),
					zap.Int("ticket", ticket.TicketId),
				)
			}

			continue
		}

		isPremium, err := d.isPremium(ctx, ticket.GuildId)
		if err != nil {
			// One bot's token says nothing about the other guilds, and its
			// premium status is unknown, so its settings must be left alone.
			if errors.Is(err, errTokenUnavailable) {
				d.logger.Error(
					"Skipping guild, whitelabel token is unavailable",
					zap.Error(err),
					zap.Uint64("guild", ticket.GuildId),
				)
				tokenFailedGuilds[ticket.GuildId] = struct{}{}

				continue
			}

			var restError request.RestError
			if errors.As(err, &restError) && restError.StatusCode == 404 {
				// Bot is not in guild
				d.logger.Info("Bot is not in guild, excluding tickets from autoclose", zap.Uint64("guild", ticket.GuildId))
				botNotInGuild.Add(ticket.GuildId)

				if err := d.db.AutoCloseExclude.Exclude(ctx, ticket.GuildId, ticket.TicketId); err != nil {
					d.logger.Error(
						"Error excluding ticket from autoclose",
						zap.Error(err),
						zap.Uint64("guild", ticket.GuildId),
						zap.Int("ticket", ticket.TicketId),
					)
				}

				continue
			} else {
				d.logger.Error(
					"Error getting premium status",
					zap.Error(err),
					zap.Uint64("guild", ticket.GuildId),
					zap.Int("ticket", ticket.TicketId),
				)

				return // Likely that the rest will fail as well
			}
		}

		if isPremium {
			// Convert message ID to timestamp for debug logging
			if ticket.LastMessageId == nil {
				d.logger.Info(
					"Queueing ticket close (no messages)",
					zap.Uint64("guild", ticket.GuildId),
					zap.Int("ticket", ticket.TicketId),
				)
			} else {
				shifted := *ticket.LastMessageId >> 22
				lastMessageTime := time.UnixMilli(int64(shifted + 1420070400000))

				d.logger.Info(
					"Queueing ticket close (timeout elapsed)",
					zap.Uint64("guild", ticket.GuildId),
					zap.Int("ticket", ticket.TicketId),
					zap.Time("last_message", lastMessageTime),
				)
			}

			d.AutoCloseQueue.Push(ticket)
		} else {
			d.logger.Info(
				"Guild does not have premium, so resetting autoclose settings",
				zap.Uint64("guild", ticket.GuildId),
				zap.Int("ticket", ticket.TicketId),
			)

			if err := d.db.PanelAutoClose.Reset(ctx, ticket.GuildId); err != nil {
				d.logger.Error(
					"Error resetting autoclose settings",
					zap.Error(err),
					zap.Uint64("guild", ticket.GuildId),
					zap.Int("ticket", ticket.TicketId),
				)
				return // Database error, likely to fail again
			}
		}
	}

	premiumCache = make(map[uint64]bool)
	botNotInGuild = collections.NewSet[uint64]()
}

func (d *Daemon) isPremium(ctx context.Context, guildId uint64) (bool, error) {
	isPremium, ok := premiumCache[guildId]
	if ok {
		return isPremium, nil
	} else { // If not cached, figure it out
		// Find token
		whitelabelBotId, isWhitelabel, err := d.db.WhitelabelGuilds.GetBotByGuild(ctx, guildId)
		if err != nil {
			return false, err
		}

		var token, keyPrefix string

		if isWhitelabel {
			res, err := d.db.Whitelabel.GetByBotId(ctx, whitelabelBotId)
			if err != nil {
				return false, err
			}

			token, err = whitelabelToken(d.tokenKeyring, res)
			if err != nil {
				return false, err
			}

			keyPrefix = fmt.Sprintf("ratelimiter:%d", whitelabelBotId)
		} else {
			token = d.conf.BotToken
			keyPrefix = "ratelimiter:public"
		}

		ratelimiter := ratelimit.NewRateLimiter(ratelimit.NewRedisStore(d.redis, keyPrefix), 1)
		premiumTier, err := d.premiumClient.GetTierByGuildId(ctx, guildId, true, token, ratelimiter)
		if err == nil {
			premiumCache[guildId] = premiumTier > premium.None
		}

		return premiumTier > premium.None, err
	}
}
