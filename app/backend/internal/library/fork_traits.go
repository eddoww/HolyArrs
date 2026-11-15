package library

import (
	"context"
	"log"

	"github.com/HolyArrs/holyarrs/app/backend/internal/core/database"
	"github.com/HolyArrs/holyarrs/app/backend/internal/mediafiles"
)

// StartOwnTraits is LoadOwnTraits at start-up. Once per install it first
// gives the traits and Pick rules a fork build moved from Sonarr and Radarr
// upstream's imported form (mediafiles.UpgradeForkRules), so nothing is
// judged with the old form. A failed upgrade is logged and tried again on
// the next start; the traits load either way.
func StartOwnTraits(ctx context.Context, st *database.Store) error {
	if _, _, err := mediafiles.UpgradeForkRules(ctx, st); err != nil {
		log.Printf("library: upgrading the moved traits and Pick rules: %v", err)
	}
	return LoadOwnTraits(ctx, st)
}
