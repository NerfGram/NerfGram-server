package bots

import (
	"context"
	"fmt"
	"time"

	"telesrv/internal/domain"
)

const upgraderBotStartText = `🎰 <b>NerfGram Star Gifts Upgrader</b>

Добро пожаловать в официальный Апгрейдер подарков NerfGram!

Здесь вы можете улучшать ваши подарки из профиля до более дорогих, редких и легендарных предметов, либо выигрывать подарки за звёзды ⭐.

Нажмите кнопку ниже, чтобы открыть Апгрейдер:`

func (s *Service) respondAsUpgrader(userID int64, body string) {
	mu := s.serviceBotReplyLock(domain.UpgraderBotUserID, userID)
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	upgradeURL := fmt.Sprintf("https://upgrade.nerfgram.space?user_id=%d", userID)
	reply := botReply{
		Text: upgraderBotStartText,
		ReplyMarkup: &domain.MessageReplyMarkup{
			Type: domain.MessageReplyMarkupInline,
			Inline: [][]domain.MarkupButton{
				{
					{
						Type: domain.MarkupButtonWebView,
						Text: "🎰 Открыть Апгрейдер",
						URL:  upgradeURL,
					},
				},
			},
		},
	}
	s.sendServiceBotReply(ctx, domain.UpgraderBotUserID, userID, reply)
}
