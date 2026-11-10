package migrate

import (
	"fmt"
	"strings"

	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

// requestSetupKeys are the request app settings requestSetup reads.
var requestSetupKeys = []string{"partial_requests", "special_episodes", "media_server", "media_server_url"}

// requestSetup plans what a request app's setup becomes here: where
// approved requests go (its default Radarr and Sonarr servers, plain and
// 4K, as the request rules' folders and Pick rules), which Library folders
// hold 4K copies and which Pick rules each folder starts new titles on,
// its rules for asking for seasons and specials, and the media server
// people sign in with.
func (b *builder) requestSetup(s *Snapshot) {
	b.cover("request_servers")
	if s.Kind != Seerr && s.Kind != Overseerr {
		return
	}
	from := b.from("settings", -1)
	as := &AskSettingsPlan{}
	var where []string
	for i, rs := range s.RequestServers {
		sf := b.from("request_servers", i)
		op := oldPath(rs.Folder)
		if op == "" {
			b.not(GroupRequests, sf, rs.Name, "it names no full folder path, so approved requests keep this server's folders")
			continue
		}
		folder, _ := MapPath(b.rules, op)
		target := &AskTarget{Folder: folder, Profile: rs.Profile, Preset: b.requestPreset(rs)}
		if rs.FourK {
			b.fourK[folder] = true
		}
		b.folderRules(folder, target)
		if !rs.Default {
			b.not(GroupRequests, sf, rs.Name, "only the default server of each kind sets where approved requests go; pick this one's folder when you approve")
			continue
		}
		slot := map[string]map[bool]**AskTarget{"movie": {false: &as.Movie, true: &as.FourK}, "show": {false: &as.Show, true: &as.ShowFourK}}[rs.Kind][rs.FourK]
		if slot == nil || *slot != nil {
			b.not(GroupRequests, sf, rs.Name, "another default server of the same kind came first")
			continue
		}
		*slot = target
		word := map[string]string{"movie": "movies", "show": "shows"}[rs.Kind]
		if rs.FourK {
			word = "4K " + word
		}
		rules := target.Profile
		if rules == "" {
			rules = target.Preset
		}
		where = append(where, fmt.Sprintf("%s to %s on %s", word, folder, rules))
		b.p.Mapped = append(b.p.Mapped, sf)
	}
	if len(where) > 0 {
		b.add(GroupRequests, Item{Key: "wishlist_targets:" + string(s.Kind), Name: "Where approved requests go", From: b.from("request_servers", -1),
			Detail: strings.Join(where, "; "), Why: "Pick rules named after the profile are used when they are here, else the ready-made ones it matched", AskSettings: as})
	}
	if v, ok := s.Settings["partial_requests"]; ok {
		if boolean(v) {
			b.p.Mapped = append(b.p.Mapped, from+".partial_requests")
		} else {
			b.not(GroupRequests, from+".partial_requests", "asking for single seasons off", "people can always ask for single seasons here")
		}
	}
	if v, ok := s.Settings["special_episodes"]; ok {
		if boolean(v) {
			b.not(GroupRequests, from+".special_episodes", "specials on", "turn on Keep specials under Settings > Movies and TV defaults if you want them")
		} else {
			b.p.Mapped = append(b.p.Mapped, from+".special_episodes")
		}
	}
	kind := str(s.Settings["media_server"])
	addr := str(s.Settings["media_server_url"])
	switch {
	case kind == "":
	case (kind == "emby" || kind == "jellyfin") && addr != "":
		name := map[string]string{"emby": "Emby", "jellyfin": "Jellyfin"}[kind]
		b.add(GroupPeople, Item{Key: "sign_in:" + kind, Name: "Sign in with " + name, From: from + ".media_server",
			Detail: "people signed in with " + name + " at " + addr + "; added off until you turn it on",
			Why:    "people who sign in with " + name + " are not joined to the moved people; link an account under its profile",
			SignIn: &SignInPlan{Kind: kind, URL: addr}})
		b.not(GroupOther, from+".media_server_url", name+" at "+addr, "its key stays in the old app: add it under Settings > Media servers to tell it about new files")
	default:
		b.not(GroupPeople, from+".media_server", kind, "people sign in with Plex here once the owner turns it on under Settings > Sign-in")
	}
}

// requestPreset is the ready-made Pick rules a request server's profile
// matched in the arr apps read with it, else 4K for a 4K server and
// Standard for the others.
func (b *builder) requestPreset(rs RequestServer) string {
	if p := b.namedPresets[rs.Kind+":"+strings.ToLower(rs.Profile)]; p != "" {
		return p
	}
	if rs.FourK {
		return mediafiles.FourKName
	}
	return mediafiles.GoodEverydayName
}

// folderRules makes target's Pick rules the default of the planned Library
// folder at path, unless another server already set it.
func (b *builder) folderRules(path string, target *AskTarget) {
	g := b.items[GroupFolders]
	for i := range g.Items {
		f := g.Items[i].Folder
		if f == nil || f.Path != path || f.Profile != "" || f.Preset != "" {
			continue
		}
		f.Profile, f.Preset = target.Profile, target.Preset
		rules := target.Profile
		if rules == "" {
			rules = target.Preset
		}
		g.Items[i].Detail += "; new titles take " + rules
	}
}
