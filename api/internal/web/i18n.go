package web

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const defaultLang = "en"

// catalog holds the UI text per language. Keys are the English meaning in
// short form; TestCatalog checks that both languages have the same keys and
// that templates use only known ones.
var catalog = map[string]map[string]string{
	"en": {
		"app.name":               "Organon",
		"login.title":            "Sign in",
		"login.token":            "API token",
		"login.submit":           "Sign in",
		"login.help":             "Create one with: organon token new --name web --scopes read,tasks:write",
		"login.invalid":          "That token was not accepted.",
		"login.noread":           "This token lacks the read scope.",
		"login.blocked":          "Too many failed attempts. Try again in a minute.",
		"nav.today":              "Today",
		"nav.tasks":              "Tasks",
		"nav.waiting":            "Waiting",
		"nav.logout":             "Sign out",
		"lang.switch":            "Language",
		"add.title":              "New task",
		"add.deadline":           "Deadline (optional)",
		"add.submit":             "Add",
		"empty.list":             "Nothing here.",
		"action.start":           "Start",
		"action.complete":        "Complete",
		"action.skip":            "Skip",
		"task.scheduled":         "Scheduled",
		"task.deadline":          "Deadline",
		"kind.scheduled":         "Scheduled today",
		"kind.past-scheduled":    "Scheduled earlier",
		"kind.deadline":          "Due",
		"kind.upcoming-deadline": "Due soon",
		"kind.event":             "Event",
		"confirm.title":          "This task repeats",
		"confirm.complete":       "Completing it moves it to its next occurrence. This cannot be undone here.",
		"confirm.skip":           "Skipping it moves it to its next occurrence. This cannot be undone here.",
		"confirm.yes":            "Yes, continue",
		"confirm.no":             "Go back",
		"notice.conflict":        "This task changed in the meantime. Check the current list and try again.",
		"notice.gone":            "This task no longer exists.",
		"notice.invalid":         "That was not accepted. Check the title and the date.",
		"notice.forbidden":       "Your token does not allow this.",
		"notice.unknown":         "No answer from the service, so it may or may not have been applied. Check the list before trying again.",
		"notice.error":           "That failed. Try again later.",
		"error.title":            "Something went wrong",
		"error.unavailable":      "The service is unavailable right now. Try again in a moment.",
		"error.forbidden":        "Your token does not allow this page.",
		"error.notfound":         "There is no such page.",
		"error.internal":         "An internal error occurred.",
		"error.home":             "Back to Today",
	},
	"ko": {
		"app.name":               "Organon",
		"login.title":            "로그인",
		"login.token":            "API 토큰",
		"login.submit":           "로그인",
		"login.help":             "토큰 만들기: organon token new --name web --scopes read,tasks:write",
		"login.invalid":          "토큰을 확인할 수 없습니다.",
		"login.noread":           "이 토큰에는 read 권한이 없습니다.",
		"login.blocked":          "실패한 시도가 너무 많습니다. 1분 뒤에 다시 시도하세요.",
		"nav.today":              "오늘",
		"nav.tasks":              "할 일",
		"nav.waiting":            "대기",
		"nav.logout":             "로그아웃",
		"lang.switch":            "언어",
		"add.title":              "새 할 일",
		"add.deadline":           "마감일 (선택)",
		"add.submit":             "추가",
		"empty.list":             "항목이 없습니다.",
		"action.start":           "시작",
		"action.complete":        "완료",
		"action.skip":            "건너뛰기",
		"task.scheduled":         "예정",
		"task.deadline":          "마감",
		"kind.scheduled":         "오늘 예정",
		"kind.past-scheduled":    "지난 예정",
		"kind.deadline":          "마감",
		"kind.upcoming-deadline": "곧 마감",
		"kind.event":             "일정",
		"confirm.title":          "반복되는 할 일입니다",
		"confirm.complete":       "완료하면 다음 회차로 넘어갑니다. 여기서는 되돌릴 수 없습니다.",
		"confirm.skip":           "건너뛰면 다음 회차로 넘어갑니다. 여기서는 되돌릴 수 없습니다.",
		"confirm.yes":            "계속하기",
		"confirm.no":             "돌아가기",
		"notice.conflict":        "그사이 이 할 일이 바뀌었습니다. 현재 목록을 확인하고 다시 시도하세요.",
		"notice.gone":            "이 할 일은 더 이상 없습니다.",
		"notice.invalid":         "처리할 수 없는 입력입니다. 제목과 날짜를 확인하세요.",
		"notice.forbidden":       "이 토큰으로는 할 수 없는 작업입니다.",
		"notice.unknown":         "서비스가 응답하지 않아 반영 여부를 알 수 없습니다. 다시 시도하기 전에 목록을 확인하세요.",
		"notice.error":           "실패했습니다. 나중에 다시 시도하세요.",
		"error.title":            "문제가 발생했습니다",
		"error.unavailable":      "지금은 서비스를 사용할 수 없습니다. 잠시 후 다시 시도하세요.",
		"error.forbidden":        "이 토큰으로는 볼 수 없는 페이지입니다.",
		"error.notfound":         "없는 페이지입니다.",
		"error.internal":         "내부 오류가 발생했습니다.",
		"error.home":             "오늘로 돌아가기",
	},
}

// translate returns the text of key in lang. A missing key is an error, which
// fails the page's rendering; TestCatalog rules that out before release.
func translate(lang, key string) (string, error) {
	if text, ok := catalog[lang][key]; ok {
		return text, nil
	}
	return "", &missingKeyError{lang, key}
}

type missingKeyError struct{ lang, key string }

func (e *missingKeyError) Error() string { return "no text for " + e.lang + "/" + e.key }

// requestLang picks the page language: the language cookie if it names a
// supported language, then Accept-Language, then English.
func requestLang(r *http.Request) string {
	if c, err := r.Cookie(langCookie); err == nil && catalog[c.Value] != nil {
		return c.Value
	}
	return acceptedLang(r.Header.Get("Accept-Language"))
}

// acceptedLang returns the supported language the header prefers most.
func acceptedLang(header string) string {
	type choice struct {
		lang string
		q    float64
	}
	var choices []choice
	for _, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		primary, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		if catalog[primary] == nil {
			continue
		}
		q := 1.0
		if name, value, ok := strings.Cut(strings.TrimSpace(params), "="); ok && strings.TrimSpace(name) == "q" {
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				continue
			}
			q = parsed
		}
		if q > 0 {
			choices = append(choices, choice{primary, q})
		}
	}
	if len(choices) == 0 {
		return defaultLang
	}
	sort.SliceStable(choices, func(i, j int) bool { return choices[i].q > choices[j].q })
	return choices[0].lang
}
