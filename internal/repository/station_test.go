package repository

import (
	"strings"
	"testing"

	"github.com/PMPRR/SCOM---Trabalho-2/internal/domain"
)

// What a user types reaches PostgreSQL as an argument, never as SQL text.
func TestStationWhereKeepsValuesOutOfTheSQL(t *testing.T) {
	owner := int64(7)
	attack := `x' OR '1'='1'; DROP TABLE stations; --`
	where, args := stationWhere(domain.StationFilter{ActiveOnly: true, OwnerID: &owner, Search: attack})

	if strings.Contains(where, "DROP") || strings.Contains(where, "'") {
		t.Errorf("the search reached the SQL text: %s", where)
	}
	want := " WHERE s.active AND s.owner_id = $1 AND (s.name ILIKE $2 OR s.external_code ILIKE $2)"
	if where != want {
		t.Errorf("where = %q\nwant    %q", where, want)
	}
	if len(args) != 2 || args[0] != owner || args[1] != "%"+attack+"%" {
		t.Errorf("args = %#v", args)
	}
}

func TestStationWhereWithoutFilter(t *testing.T) {
	if where, args := stationWhere(domain.StationFilter{}); where != "" || args != nil {
		t.Errorf("where = %q, args = %v; want nothing", where, args)
	}
}

func TestEscapeLikeMatchesWildcardsLiterally(t *testing.T) {
	if got := escapeLike(`50%_a\b`); got != `50\%\_a\\b` {
		t.Errorf("escapeLike = %q", got)
	}
}
