package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
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
	//452639799,
	//1222964929, // основной инженер/админ
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
			userID := callback.From.ID

			if strings.HasPrefix(userStates[userID], "awaiting_review_done_") {
				filename := strings.TrimPrefix(userStates[userID], "awaiting_review_done_")
				userStates[userID] = ""

				parts := strings.Split(filename, "_")
				ticketID := parts[1]
				engineerID := parts[2]

				clientUsername := update.Message.From.UserName
				if clientUsername == "" {
					clientUsername = fmt.Sprintf("%d", userID)
				}
				text := update.Message.Text
				reviewLine := fmt.Sprintf(
					"ВЫПОЛНЕНО | Заявка: %s | Клиент: %s | Инженер: %s | Комментарий: %s\n",
					ticketID, clientUsername, engineerID, text,
				)
				f, err := os.OpenFile("reviews_done.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
				if err == nil {
					f.WriteString(reviewLine)
					f.Close()
				}
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Спасибо за ваш отзыв!"))
				continue
			}

			if strings.HasPrefix(data, "review|") {
				filename := strings.TrimPrefix(data, "review|")
				// Сохраняем в userStates[userID] что ждем отзыв по этой заявке
				userStates[callback.From.ID] = "awaiting_review_" + filename
				// Удаляем сообщение с кнопками
				del := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
				bot.Request(del)
				// Просим пользователя оставить отзыв
				bot.Send(tgbotapi.NewMessage(callback.From.ID, "Пожалуйста, напишите отзыв об инженере по этой заявке:"))
				// Pop-up
				cb := tgbotapi.NewCallback(callback.ID, "Ожидаем ваш отзыв")
				bot.Request(cb)
				continue
			}

			if strings.HasPrefix(data, "done|") {
				filename := strings.TrimPrefix(data, "done|")
				newFilename := strings.Replace(filename, ".txt", "_done.txt", 1)
				err := os.Rename(filename, newFilename)
				if err != nil {
					bot.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "❌ Не удалось завершить заявку."))
				} else {
					// Удаляем сообщение с кнопками
					del := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
					bot.Request(del)
					// Pop-up
					cb := tgbotapi.NewCallback(callback.ID, "✅ Заявка выполнена! Оставьте отзыв.")
					bot.Request(cb)
					// Уведомление инженерам
					ticketID := strings.Split(strings.TrimPrefix(filename, "zayavka_"), "_")[0]
					msg := fmt.Sprintf("✅ Заявка №%s отмечена как выполненная!", ticketID)
					notifyEngineers(bot, msg, nil)
					// --- Ждём отзыв ---
					nameParts := strings.Split(filename, "_")
					if len(nameParts) >= 4 {
						clientID, err := strconv.ParseInt(nameParts[3], 10, 64)
						if err == nil {
							userStates[clientID] = "awaiting_review_done_" + filename
							bot.Send(tgbotapi.NewMessage(clientID, "Пожалуйста, оставьте отзыв об инженере по вашей выполненной заявке:"))
						}
					}
				}
				continue
			}

			if strings.HasPrefix(data, "delete|") {
				filename := strings.TrimPrefix(data, "delete|")
				// Здесь удаляешь или переименовываешь файл:
				newFilename := strings.Replace(filename, ".txt", "_deleted.txt", 1)
				err := os.Rename(filename, newFilename)
				if err != nil {
					bot.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "❌ Не удалось скрыть заявку."))
				} else {
					// Удалить сообщение из чата
					del := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
					bot.Request(del)
					// Показываем pop-up пользователю
					cb := tgbotapi.NewCallback(callback.ID, "✅ Заявка удалена.")
					bot.Request(cb)
					// Отправить уведомление инженерам
					ticketID := strings.Split(strings.TrimPrefix(filename, "zayavka_"), "_")[0]
					adminMsg := fmt.Sprintf("❗ Клиент удалил заявку №%s", ticketID)
					notifyEngineers(bot, adminMsg, nil)
				}
				continue
			}

			if strings.HasPrefix(data, "take|") {
				ticketID := strings.TrimPrefix(data, "take|")
				files, _ := filepath.Glob(fmt.Sprintf("zayavka_%s_*.txt", ticketID))
				if len(files) == 0 {
					bot.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "❌ Заявка не найдена."))
					continue
				}
				oldName := files[0]
				if strings.Contains(oldName, "_taken_") {
					// Уже занято — удаляем кнопку, попап
					del := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
					bot.Request(del)
					cb := tgbotapi.NewCallback(callback.ID, "⛔ Заявка уже взята другим инженером")
					bot.Request(cb)
					continue
				}
				from := callback.From
				username := from.UserName
				if username == "" {
					username = "инженер без username"
				}
				newName := strings.Replace(oldName, ".txt", fmt.Sprintf("_taken_%d.txt", from.ID), 1)
				_ = os.Rename(oldName, newName)

				// Читаем username клиента из файла заявки
				content, _ := os.ReadFile(newName)
				clientUsername := extractBetween(string(content), "от @", " (ID ")
				if clientUsername == "" || clientUsername == "Без username" {
					clientUsername = "(нет username)"
				}

				// Уведомление всем инженерам
				msg := fmt.Sprintf("🛠 Заявку №%s взял в работу инженер @%s", ticketID, username)
				notifyEngineers(bot, msg, nil)

				// Удалить сообщение кнопки у откликнувшегося
				del := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
				bot.Request(del)

				cb := tgbotapi.NewCallback(callback.ID, "Вы взяли заявку в работу")
				bot.Request(cb)

				// Перебросить инженера в чат с клиентом (отправить ему ссылку)
				if clientUsername != "(нет username)" {
					contactURL := fmt.Sprintf("https://t.me/%s", clientUsername)
					linkMsg := fmt.Sprintf("Свяжитесь с клиентом: @%s\n%s", clientUsername, contactURL)
					bot.Send(tgbotapi.NewMessage(callback.From.ID, linkMsg))
				}
				continue
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

			replyMarkup := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("🛠 Взять в работу", fmt.Sprintf("take|%s", ticketID)),
				),
			)

			notifyEngineers(bot, adminMsg, replyMarkup)

		default:
			if strings.HasPrefix(userStates[userID], "awaiting_review_done_") {
				filename := strings.TrimPrefix(userStates[userID], "awaiting_review_done_")
				userStates[userID] = "" // сбрасываем состояние

				// Достаем из имени файла данные (номер заявки, id инженера)
				parts := strings.Split(filename, "_")
				ticketID := parts[1]
				engineerID := parts[2]

				clientUsername := update.Message.From.UserName
				if clientUsername == "" {
					clientUsername = fmt.Sprintf("%d", userID)
				}

				// Записываем отзыв
				reviewLine := fmt.Sprintf(
					"ВЫПОЛНЕНО | Заявка: %s | Клиент: %s | Инженер: %s | Комментарий: %s\n",
					ticketID, clientUsername, engineerID, text,
				)
				f, err := os.OpenFile("reviews_done.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
				if err == nil {
					f.WriteString(reviewLine)
					f.Close()
				}
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Спасибо! Ваш комментарий принят."))
				break
			}
			// --- Проверка: ожидаем отзыв? ---
			if strings.HasPrefix(userStates[userID], "awaiting_review_") {
				filename := strings.TrimPrefix(userStates[userID], "awaiting_review_")
				userStates[userID] = "" // сбрасываем состояние

				// Достаем из имени файла данные (номер заявки, id инженера)
				parts := strings.Split(filename, "_")
				ticketID := parts[1]
				engineerID := parts[2]

				clientUsername := update.Message.From.UserName
				if clientUsername == "" {
					clientUsername = fmt.Sprintf("%d", userID)
				}

				// Записываем отзыв
				reviewLine := fmt.Sprintf(
					"НЕ ВЫПОЛНЕНО | Заявка: %s | Клиент: %s | Инженер: %s | Комментарий: %s\n",
					ticketID, clientUsername, engineerID, text,
				)
				f, err := os.OpenFile("reviews.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
				if err == nil {
					f.WriteString(reviewLine)
					f.Close()
				}
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Спасибо! Ваш комментарий принят."))
				break
			}

			switch text {
			case "📋 Создать заявку":
				userStates[userID] = "awaiting_name"
				bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Введите ваше имя:"))

			case "🗂 Мои заявки":
				pattern := fmt.Sprintf("zayavka_*_%d*.txt", userID)
				files, err := filepath.Glob(pattern)
				if err != nil || len(files) == 0 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Вы пока не создавали заявок."))
					break
				}

				shown := 0
				for _, file := range files {
					if strings.Contains(file, "_deleted") || strings.Contains(file, "_done") {
						continue
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

					isClient := strings.Contains(file, fmt.Sprintf("_%d", userID))             // заявка создана этим юзером?
					isEngineer := strings.Contains(file, fmt.Sprintf("_taken_%d.txt", userID)) // заявку взял этот инженер?

					isTaken := strings.Contains(file, "_taken_")

					if isClient && !isTaken {
						// Кнопки для клиента, если заявка ещё не в работе
						callbackData := fmt.Sprintf("delete|%s", filepath.Base(file))
						msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
							tgbotapi.NewInlineKeyboardRow(
								tgbotapi.NewInlineKeyboardButtonData("❌ Удалить", callbackData),
							),
						)
					} else if isClient && isTaken {
						// Заявка клиента — уже в работе, кнопок нет, статус показываем
						short += "\n\n⏳ Ваша заявка в обработке инженером."
						msg.Text = short
					} else if isEngineer {
						// Заявка, которую инженер взял в работу: просто показываем детали (без кнопок)
						short += "\n\n📢 Эта заявка сейчас у вас в работе."
						msg.Text = short
					}

					bot.Send(msg)
					shown++
				}

				if shown == 0 {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "У вас нет активных заявок."))
				}
				break

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
