package pg

import (
	"fmt"
	"net/url"
)

type URLParts struct {
	User     string
	Password string
	Host     string
	Port     int32
	DB       string
	SSLMode  string
}

func ConnectURL(p URLParts) string {
	if p.SSLMode == "" {
		p.SSLMode = "require"
	}
	if p.Port == 0 {
		p.Port = 5432
	}
	u := url.URL{
		Scheme:   "postgresql",
		User:     url.UserPassword(p.User, p.Password),
		Host:     fmt.Sprintf("%s:%d", p.Host, p.Port),
		Path:     "/" + p.DB,
		RawQuery: "sslmode=" + p.SSLMode,
	}
	return u.String()
}
