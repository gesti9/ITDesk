package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Структура заявки в процессе создания
type TempTicket struct {
	Name        string
	Description string
	Address     string
}

var userStates = make(map[int64]string)
var tempTickets = make(map[int64]*TempTicket)

var engineerIDs = []int64{
	452639799, // основной инженер/админ
	// другие ID добавь сюда
}

func notifyEngineers(bot *tgbotapi.BotAPI, msgText string, replyMarkup interface{}) {
	for _, id := range engineerIDs {
		msg := tgbotapi.NewMessage(id, msgText)
		if replyMarkup != nil {
			msg.ReplyMarkup = replyMarkup
		}
		bot.Send(msg)
	}
}

func main() {
	botToken := "8176658518:AAFTsga4y3XAmLOrpvrsDdTSX1WSjnDM3RQ"

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	//bot.Debug = true
	log.Printf("Бот авторизован как %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {

		if update.CallbackQuery != nil {
			callback := update.CallbackQuery
			data := callback.Data

			if strings.HasPrefix(data, "delete|") {
				filename := strings.TrimPrefix(data, "delete|")
				newFilename := strings.Replace(filename, ".txt", "_deleted.txt", 1)

				err := os.Rename(filename, newFilename)
				if err != nil {
					bot.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "❌ Не удалось скрыть заявку."))
				} else {
					// Удаляем сообщение из чата
					del := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
					_, err := bot.Request(del)
					if err != nil {
						log.Println("Ошибка при удалении сообщения:", err)
					}

					// Показываем pop-up пользователю
					cb := tgbotapi.NewCallback(callback.ID, "✅ Заявка удалена.")
					_, err = bot.Request(cb)
					if err != nil {
						log.Println("Ошибка при отправке callback:", err)
					}

					// Отправляем уведомление инженерам
					ticketID := strings.Split(strings.TrimPrefix(filename, "zayavka_"), "_")[0]
					adminMsg := fmt.Sprintf("❗ Клиент удалил заявку №%s", ticketID)
					notifyEngineers(bot, adminMsg, nil)
				}
			}

			continue // важно!
		}

		// 2. Только после этого обрабатываем обычные сообщения
		if update.Message == nil {
			continue
		}

		userID := update.Message.From.ID
		text := update.Message.Text

		// Обработка команды /start
		if update.Message.IsCommand() {
			switch update.Message.Command() {
			case "start":
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Выберите действие:")
				msg.ReplyMarkup = mainMenuKeyboard()
				bot.Send(msg)
				userStates[userID] = ""
			}
			continue
		}

		// Проверка состояния пользователя
		switch userStates[userID] {
		case "awaiting_name":
			tempTickets[userID] = &TempTicket{}
			tempTickets[userID].Name = text
			userStates[userID] = "awaiting_description"
			bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Опишите проблему:"))

		case "awaiting_description":
			if tempTickets[userID] == nil {
				tempTickets[userID] = &TempTicket{}
			}
			tempTickets[userID].Description = text
			userStates[userID] = "awaiting_address"
			bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Теперь введите адрес, где находится техника:"))

		case "awaiting_address":
			tempTickets[userID].Address = text
			userStates[userID] = ""
			ticket, ok := tempTickets[userID]
			if !ok || ticket == nil {
				log.Println("❗ Ошибка: заявка не найдена в tempTickets")
				continue
			}
			delete(tempTickets, userID)

			from := update.Message.From
			username := from.UserName
			if username == "" {
				username = "Без username"
			}

			// ✅ Создание текстового файла
			now := time.Now()
			ticketID := now.Format("20060102150405") // создаём уникальный ID по дате/времени

			fileName := fmt.Sprintf("zayavka_%s_%d.txt", ticketID, from.ID)

			fileContent := fmt.Sprintf(
				"Заявка от @%s (ID %d)\nДата: %s\n\nОписание:\n%s\n\nАдрес:\n%s\n",
				username,
				from.ID,
				now.Format("02.01.2006 15:04"),
				ticket.Description,
				ticket.Address,
			)
			err := os.WriteFile(fileName, []byte(fileContent), 0644)
			if err != nil {
				log.Println("Ошибка при записи в файл:", err)
			}

			// ✅ Подтверждение клиенту
			confirm := fmt.Sprintf("✅ Заявка создана!\n\n📋 Описание: %s\n📍 Адрес: %s", ticket.Description, ticket.Address)
			bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, confirm))
			bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Вернуться в меню: /start"))

			// ✅ Отправка админу с кнопкой "Связаться"
			adminMsg := fmt.Sprintf(
				"📨 Новая заявка №%s:\n\n👤 %s\n📋 %s\n📍 %s",
				ticketID,
				ticket.Name,
				ticket.Description,
				ticket.Address,
			)

			var replyMarkup tgbotapi.InlineKeyboardMarkup
			if username != "Без username" {
				contactURL := fmt.Sprintf("https://t.me/%s", username)
				replyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonURL("💬 Связаться", contactURL),
					),
				)
			}

			notifyEngineers(bot, adminMsg, replyMarkup)

		default:
			switch text {
			case "📋 Создать заявку":
				userStates[userID] = "awaiting_name"
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Введите ваше имя:"))

			case "🗂 Мои заявки":
				pattern := fmt.Sprintf("zayavka_*_%d.txt", userID)
				files, err := filepath.Glob(pattern)
				if err != nil || len(files) == 0 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Вы пока не создавали заявок."))
					break
				}

				for _, file := range files {
					if strings.Contains(file, "_deleted") {
						continue // ❌ пропустить удалённую заявку
					}

					contentBytes, err := os.ReadFile(file)
					if err != nil {
						log.Println("Ошибка чтения файла:", err)
						continue
					}

					content := string(contentBytes)
					description := extractBetween(content, "Описание:\n", "\n\nАдрес:")
					address := extractAfter(content, "Адрес:\n")

					short := fmt.Sprintf("📋 %s\n📍 %s", description, address)
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, short)
					callbackData := fmt.Sprintf("delete|%s", filepath.Base(file)) // только имя файла
					msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("❌ Удалить", callbackData),
						),
					)
					bot.Send(msg)
				}

			case "⭐ Оценить инженера":
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Нет завершённых заявок для оценки."))

			default:
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Я вас не понял. Нажмите /start."))
			}
		}
	}
}

// Главное меню клиента
func mainMenuKeyboard() tgbotapi.ReplyKeyboardMarkup {
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📋 Создать заявку"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🗂 Мои заявки"),
			tgbotapi.NewKeyboardButton("⭐ Оценить инженера"),
		),
	)
	keyboard.ResizeKeyboard = true
	return keyboard
}

func extractBetween(text, start, end string) string {
	s := strings.Index(text, start)
	if s == -1 {
		return ""
	}
	s += len(start)
	e := strings.Index(text[s:], end)
	if e == -1 {
		return ""
	}
	return strings.TrimSpace(text[s : s+e])
}

func extractAfter(text, start string) string {
	s := strings.Index(text, start)
	if s == -1 {
		return ""
	}
	s += len(start)
	return strings.TrimSpace(text[s:])
}
