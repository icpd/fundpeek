package httpclient

import (
	"regexp"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

func New(baseURL string) *resty.Client {
	return resty.New().
		SetBaseURL(baseURL).
		SetLogger(DiscardLogger()).
		SetTimeout(30*time.Second).
		SetRetryCount(2).
		SetRetryWaitTime(500*time.Millisecond).
		SetHeader("User-Agent", "fundpeek/0.1")
}

func DiscardLogger() resty.Logger {
	return discardLogger{}
}

func SafeBody(body []byte) string {
	const max = 512
	s := strings.TrimSpace(string(body))
	if len(s) > max {
		s = s[:max] + "..."
	}
	for _, pattern := range sensitivePatterns {
		s = pattern.ReplaceAllString(s, `${1}"<redacted>"`)
	}
	return s
}

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)("(?:access_token|refresh_token|token|authorization|apikey|api_key)"\s*:\s*)"[^"]*"`),
}

type discardLogger struct{}

func (discardLogger) Errorf(string, ...interface{}) {}
func (discardLogger) Warnf(string, ...interface{})  {}
func (discardLogger) Debugf(string, ...interface{}) {}
