package secretsfile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	domain "github.com/HolyArrs/holyarrs/app/backend/internal/core/model"
	"github.com/HolyArrs/holyarrs/app/backend/internal/core/secrets"
)

// Move credentials live in the move_credentials table: an old app's
// address and key (qBittorrent: or its sign-in), saved from the secrets
// file or by a successful read with a typed key, as one vault secret
// each. They are kept for "Read again" until a move finishes.

// MoveKinds are the old apps a credential may be saved for.
var MoveKinds = []string{"radarr", "sonarr", "lidarr", "prowlarr", "bazarr", "seerr", "overseerr", "qbittorrent", "sabnzbd"}

// MoveCredential is one old app's address and key (or sign-in).
type MoveCredential struct {
	Address           string `json:"address"`
	APIKey            string `json:"api_key"`
	Username          string `json:"username,omitempty"`
	Password          string `json:"password,omitempty"`
	AllowLocalNetwork bool   `json:"allow_local_network"`
}

// SavedMove is what the Move screen is told about a saved credential:
// never the key.
type SavedMove struct {
	Kind              string `json:"kind"`
	Address           string `json:"address"`
	AllowLocalNetwork bool   `json:"allow_local_network"`
}

func validMoveKind(k string) bool {
	for _, x := range MoveKinds {
		if x == k {
			return true
		}
	}
	return false
}

// moveAddress accepts only an old app's plain base address. Credentials
// belong in the vault fields, never in an address returned to the screen.
func moveAddress(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
		return "", errors.New("the old app address must be http or https, with no user, password, query or fragment")
	}
	u.Host = strings.ToLower(u.Host)
	return strings.TrimRight(u.String(), "/"), nil
}

// SaveMoveCredential stores (or replaces) the credential for an old app at
// its address; another app of the same kind keeps its own. A key kept before
// keys were kept per address is replaced too, as it cannot be told apart
// without opening it.
func SaveMoveCredential(ctx context.Context, st *database.Store, v *secrets.Vault, kind string, c MoveCredential, by domain.Actor) error {
	if !validMoveKind(kind) {
		return errors.New("unknown old app")
	}
	if v == nil {
		return errors.New("the secrets vault is not open")
	}
	address, err := moveAddress(c.Address)
	if err != nil {
		return err
	}
	c.Address = address
	b, _ := json.Marshal(c)
	return st.Write(ctx, func(tx *sql.Tx) error {
		for _, at := range []string{address, ""} {
			if err := dropMove(ctx, tx, kind, at); err != nil {
				return err
			}
		}
		id, err := v.PutTx(tx, "move_credential", string(b), by)
		if err != nil {
			return err
		}
		ab, _ := json.Marshal(by)
		return insertMoveCredential(ctx, tx, kind, address, id, database.FormatTime(time.Now()), string(ab))
	})
}

// ReadMoveCredential reads the credential for an old app at address (""
// for the newest one kept for the app) without using it up.
func ReadMoveCredential(ctx context.Context, st *database.Store, v *secrets.Vault, kind, address string) (MoveCredential, bool, error) {
	c, _, found, err := ReadMoveCredentialVersion(ctx, st, v, kind, address)
	return c, found, err
}

// MoveCredentialVersion pins the stored secret a credential was read from, so a
// later write can tell whether it changed in between.
type MoveCredentialVersion struct {
	SecretID int64
}

// ReadMoveCredentialVersion also returns the immutable vault row identity
// that a successful read must still own when it consumes the credential.
// address picks one app of the kind; "" picks the newest one kept.
func ReadMoveCredentialVersion(ctx context.Context, st *database.Store, v *secrets.Vault, kind, address string) (MoveCredential, MoveCredentialVersion, bool, error) {
	if v == nil || !validMoveKind(kind) {
		return MoveCredential{}, MoveCredentialVersion{}, false, nil
	}
	want := ""
	if address != "" {
		var err error
		if want, err = moveAddress(address); err != nil {
			return MoveCredential{}, MoveCredentialVersion{}, false, nil
		}
	}
	rows, err := moveCredentialRows(ctx, st.Read(), kind)
	if err != nil {
		return MoveCredential{}, MoveCredentialVersion{}, false, err
	}
	for _, row := range rows {
		if want != "" && row.address != "" && row.address != want {
			continue
		}
		c, err := openMoveCredential(ctx, v, row.secretID)
		if err != nil {
			return MoveCredential{}, MoveCredentialVersion{}, false, err
		}
		if want != "" && c.Address != want {
			continue
		}
		return c, MoveCredentialVersion{SecretID: row.secretID}, true, nil
	}
	return MoveCredential{}, MoveCredentialVersion{}, false, nil
}

// openMoveCredential reads one kept credential from the vault.
func openMoveCredential(ctx context.Context, v *secrets.Vault, secretID int64) (MoveCredential, error) {
	var c MoveCredential
	raw, _, err := v.GetVersion(ctx, secretID)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return c, errors.New("the saved credential could not be read")
	}
	address, err := moveAddress(c.Address)
	if err != nil {
		return MoveCredential{}, err
	}
	c.Address = address
	return c, nil
}

// ListMoveCredentials says which old apps have a saved credential, with
// their addresses; never a key. Two apps of one kind are listed apart.
func ListMoveCredentials(ctx context.Context, st *database.Store, v *secrets.Vault) ([]SavedMove, error) {
	out := []SavedMove{}
	if v == nil {
		return out, nil
	}
	for _, k := range MoveKinds {
		rows, err := moveCredentialRows(ctx, st.Read(), k)
		if err != nil {
			return out, err
		}
		for _, row := range rows {
			c, err := openMoveCredential(ctx, v, row.secretID)
			if err != nil {
				return out, err
			}
			out = append(out, SavedMove{Kind: k, Address: c.Address, AllowLocalNetwork: c.AllowLocalNetwork})
		}
	}
	return out, nil
}

// dropMove deletes the credential kept for one app at one address column
// value, with its secret.
func dropMove(ctx context.Context, tx *sql.Tx, kind, address string) error {
	rows, err := moveCredentialRows(ctx, tx, kind)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.address != address {
			continue
		}
		if err := secrets.DeleteTx(ctx, tx, row.secretID); err != nil {
			return err
		}
		return deleteMoveCredential(ctx, tx, kind, address)
	}
	return nil
}

// ForgetMoveCredential deletes the credentials of one old app kind or, with
// kind empty, all of them. It says how many apps lost a credential.
func ForgetMoveCredential(ctx context.Context, st *database.Store, kind string) (int, error) {
	n := 0
	err := st.Write(ctx, func(tx *sql.Tx) error {
		for _, k := range MoveKinds {
			if kind != "" && k != kind {
				continue
			}
			rows, err := moveCredentialRows(ctx, tx, k)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				continue
			}
			for _, row := range rows {
				if err := dropMove(ctx, tx, k, row.address); err != nil {
					return err
				}
			}
			n++
		}
		return nil
	})
	return n, err
}

// ForgetMoveCredentialAt deletes the credential kept for one old app at one
// address, leaving another app of the same kind its own. A key kept before
// keys were kept per address goes too when its secret names that address.
func ForgetMoveCredentialAt(ctx context.Context, st *database.Store, v *secrets.Vault, kind, address string) error {
	want, err := moveAddress(address)
	if err != nil || !validMoveKind(kind) {
		return nil
	}
	legacy := false
	if v != nil {
		if rows, err := moveCredentialRows(ctx, st.Read(), kind); err == nil {
			for _, row := range rows {
				if row.address != "" {
					continue
				}
				if c, err := openMoveCredential(ctx, v, row.secretID); err == nil && c.Address == want {
					legacy = true
				}
			}
		}
	}
	return st.Write(ctx, func(tx *sql.Tx) error {
		if err := dropMove(ctx, tx, kind, want); err != nil {
			return err
		}
		if legacy {
			return dropMove(ctx, tx, kind, "")
		}
		return nil
	})
}
