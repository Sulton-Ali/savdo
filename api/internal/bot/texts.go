package bot

import "fmt"

// texts is one locale's worth of static, non-LLM copy (D-113: "static
// fallback texts exist... in api/internal/bot/texts.go keyed by
// locale — Go has no access to the i18n package"). Every field is a
// plain string or a small format helper; no %-verb text is stored
// pre-formatted, so a caller can never forget an argument silently.
type texts struct {
	greeting             string // %s = shop name
	unknownCommand       string
	rateLimited          string
	fallback             string // %s = shop name; contact details appended separately when known
	startLinkUnavailable string
	startLinkFailed      string
	startLinkSuccess     string
	hoursUnavailable     string
	hoursNoDaysHeader    string
	addressUnavailable   string
	catalogUnavailable   string
	catalogEmpty         string
	catalogHeader        string

	dayNames map[string]string
}

var textsByLocale = map[string]texts{
	"uz": {
		greeting:             "Salom! Men \"%s\" do'konining Telegram yordamchisiman. Mahsulotlar, narxlar, mavjudligi, ish vaqti, manzil va aloqa haqida so'rashingiz mumkin.\n\nBuyruqlar: /hours — ish vaqti, /address — manzil, /catalog — bo'limlar.",
		unknownCommand:       "Bu buyruqni tushunmadim. /hours, /address yoki /catalog dan foydalaning, yoki savolingizni yozing.",
		rateLimited:          "Bir soatda savollar soni chegarasiga yetdingiz. Iltimos, birozdan so'ng qayta yozing.",
		fallback:             "Men faqat \"%s\" do'konining mahsulotlari, narxlari, mavjudligi, ish vaqti, manzili va aloqalari bo'yicha yordam bera olaman.",
		startLinkUnavailable: "Hisobni bog'lash hozircha ilovada mavjud emas.",
		startLinkFailed:      "Kodni tasdiqlab bo'lmadi. Kodni admin panelidan qaytadan oling.",
		startLinkSuccess:     "Telegram hisobingiz muvaffaqiyatli bog'landi.",
		hoursUnavailable:     "Ish vaqti hozircha ko'rsatilmagan.",
		hoursNoDaysHeader:    "Ish vaqti:",
		addressUnavailable:   "Manzil hozircha ko'rsatilmagan.",
		catalogUnavailable:   "Bo'limlar ro'yxatini olib bo'lmadi.",
		catalogEmpty:         "Hozircha bo'limlar yo'q.",
		catalogHeader:        "Bo'limlar:",

		dayNames: map[string]string{
			"mon": "Dushanba", "tue": "Seshanba", "wed": "Chorshanba", "thu": "Payshanba",
			"fri": "Juma", "sat": "Shanba", "sun": "Yakshanba",
		},
	},
	"ru": {
		greeting:             "Здравствуйте! Я Telegram-помощник магазина \"%s\". Спросите про товары, цены, наличие, часы работы, адрес и контакты.\n\nКоманды: /hours — часы работы, /address — адрес, /catalog — разделы.",
		unknownCommand:       "Не понял эту команду. Используйте /hours, /address или /catalog, либо напишите вопрос.",
		rateLimited:          "Вы достигли лимита вопросов за час. Пожалуйста, напишите чуть позже.",
		fallback:             "Я могу помочь только с товарами, ценами, наличием, часами работы, адресом и контактами магазина \"%s\".",
		startLinkUnavailable: "Привязка аккаунта пока недоступна в боте.",
		startLinkFailed:      "Не удалось подтвердить код. Получите новый код в панели администратора.",
		startLinkSuccess:     "Ваш Telegram-аккаунт успешно привязан.",
		hoursUnavailable:     "Часы работы пока не указаны.",
		hoursNoDaysHeader:    "Часы работы:",
		addressUnavailable:   "Адрес пока не указан.",
		catalogUnavailable:   "Не удалось получить список разделов.",
		catalogEmpty:         "Разделов пока нет.",
		catalogHeader:        "Разделы:",

		dayNames: map[string]string{
			"mon": "Понедельник", "tue": "Вторник", "wed": "Среда", "thu": "Четверг",
			"fri": "Пятница", "sat": "Суббота", "sun": "Воскресенье",
		},
	},
	"en": {
		greeting:             "Hello! I'm the Telegram assistant for \"%s\". Ask me about products, prices, availability, opening hours, address and contacts.\n\nCommands: /hours — opening hours, /address — address, /catalog — categories.",
		unknownCommand:       "I didn't understand that command. Try /hours, /address or /catalog, or just type your question.",
		rateLimited:          "You've reached the hourly question limit. Please write again in a little while.",
		fallback:             "I can only help with %s's products, prices, availability, opening hours, address and contacts.",
		startLinkUnavailable: "Account linking isn't available in the bot yet.",
		startLinkFailed:      "That code could not be confirmed. Get a new one from the admin panel.",
		startLinkSuccess:     "Your Telegram account is now linked.",
		hoursUnavailable:     "Opening hours aren't published yet.",
		hoursNoDaysHeader:    "Opening hours:",
		addressUnavailable:   "The address isn't published yet.",
		catalogUnavailable:   "Could not load the category list.",
		catalogEmpty:         "No categories yet.",
		catalogHeader:        "Categories:",

		dayNames: map[string]string{
			"mon": "Monday", "tue": "Tuesday", "wed": "Wednesday", "thu": "Thursday",
			"fri": "Friday", "sat": "Saturday", "sun": "Sunday",
		},
	},
}

// localeTexts returns locale's texts, falling back to uz for anything
// resolveCommandLocale would never actually produce (defensive only —
// mirrors D-104's own uz fallback for content).
func localeTexts(locale string) texts {
	if t, ok := textsByLocale[locale]; ok {
		return t
	}
	return textsByLocale["uz"]
}

// fallbackText builds O-24's shared "out of scope / budget / provider
// error" reply: the fixed per-locale sentence, plus the shop's phone and
// Telegram contact when the caller has them (O-24: "gives the phone and
// Telegram link from the contacts block"). contactLine is pre-built by
// the caller (commands.go/update.go) since building it needs a
// content.Service.Resolve call this package keeps out of texts.go on
// purpose (no I/O in this file).
func fallbackText(shopName, locale, contactLine string) string {
	base := fmt.Sprintf(localeTexts(locale).fallback, shopName)
	if contactLine == "" {
		return base
	}
	return base + "\n" + contactLine
}
