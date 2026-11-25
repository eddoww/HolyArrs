package sources

import (
	"strings"

	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
)

// fillSiteLogin keeps how a built-in site Source signs in, in its Extra:
// the user id, the sign-in way and the browser's user agent (upstream's),
// and the fork's freeleech choice (fillSignIn).
func fillSiteLogin(in Input, src *domain.Source, site Site, key string) error {
	if in.SiteUser != nil {
		user := strings.TrimSpace(*in.SiteUser)
		if len(user) > 20 || strings.Trim(user, "0123456789") != "" {
			return ErrSiteUser
		}
		if user == "" {
			delete(src.Extra, extraSiteUser)
		} else {
			src.Extra[extraSiteUser] = user
		}
	}
	if in.SiteLogin != nil {
		switch strings.TrimSpace(*in.SiteLogin) {
		case "", "passkey":
			delete(src.Extra, extraSiteLogin)
		case "cookie":
			if site.ID != "iptorrents" {
				return ErrSiteLogin
			}
			src.Extra[extraSiteLogin] = "cookie"
		default:
			return ErrSiteLogin
		}
	}
	if in.SiteAgent != nil {
		agent := strings.TrimSpace(*in.SiteAgent)
		if len(agent) > 500 || strings.ContainsAny(agent, "\r\n") {
			return ErrSiteLogin
		}
		if agent == "" {
			delete(src.Extra, extraSiteAgent)
		} else {
			src.Extra[extraSiteAgent] = agent
		}
	}
	return fillSignIn(in, src, site, key)
}
