// Package cocapi looks up a player or a clan by tag against Supercell's
// public Clash of Clans API - the Lookup page's data source, entirely
// separate from the village-export format the rest of this app reads.
//
// Field names in realClient below are modeled from the API's publicly
// documented schema, not verified against a live response - this session
// has no COC_API_TOKEN to test with, so the real path is code-complete but
// unexercised. Verify clanCapitalContributions and the raid-season shape
// against an actual response once a token exists; mockClient is what every
// test and manual check in this repository actually runs against.
package cocapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Hero is one hero's level - home village only; the Lookup page has no use
// for Builder Base heroes.
type Hero struct {
	Name  string `json:"name"`
	Level int    `json:"level"`
	Max   int    `json:"maxLevel"`
}

// Player is what a tag-lookup preview shows before a full export - a
// glimpse, not a replacement for one. CapitalContribution is the only Clan
// Capital figure confirmed at player granularity (a lifetime total); there
// is no per-district detail available from this data source.
type Player struct {
	Tag                 string `json:"tag"`
	Name                string `json:"name"`
	TownHallLevel       int    `json:"townHallLevel"`
	Trophies            int    `json:"trophies"`
	Heroes              []Hero `json:"heroes"`
	CapitalContribution int    `json:"capitalContribution"`
	// Mock is true only for mockClient's canned responses - see New. The
	// Lookup page must show a visible "sample data" notice whenever this is
	// true, never render it as if it were a real lookup.
	Mock bool `json:"mock,omitempty"`
}

// ClanMember is one row of a clan roster. Level is the player's own
// experience level, not a Town Hall level - the public API does not expose
// a member's Town Hall from the clan endpoint, only from a full player
// lookup on their individual tag.
type ClanMember struct {
	Tag      string `json:"tag"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Level    int    `json:"level"`
	Trophies int    `json:"trophies"`
}

// RaidMember is one member's contribution within a single Raid Weekend.
type RaidMember struct {
	Name                   string `json:"name"`
	CapitalResourcesLooted int    `json:"capitalResourcesLooted"`
	Attacks                int    `json:"attacks"`
}

// RaidSeason is one completed Raid Weekend for a clan.
type RaidSeason struct {
	EndTime          string       `json:"endTime"`
	CapitalTotalLoot int          `json:"capitalTotalLoot"`
	OffensiveReward  int          `json:"offensiveReward"`
	Members          []RaidMember `json:"members"`
}

// Clan is a roster plus recent Raid Weekend history - only what the Lookup
// page needs, not every field the real API returns. There is no confirmed
// way to get per-district Capital building levels from this API at all
// (only points/contributions/league) - the Lookup page says so rather than
// pretending to more detail than this data source actually has.
type Clan struct {
	Tag           string       `json:"tag"`
	Name          string       `json:"name"`
	Level         int          `json:"level"`
	Points        int          `json:"points"`
	CapitalPoints int          `json:"capitalPoints"`
	Members       []ClanMember `json:"members"`
	RaidSeasons   []RaidSeason `json:"raidSeasons"`
	// Mock is true only for mockClient's canned responses - see Player.Mock.
	Mock bool `json:"mock,omitempty"`
}

// Client looks up a player or a clan by tag.
type Client interface {
	Player(ctx context.Context, tag string) (Player, error)
	Clan(ctx context.Context, tag string) (Clan, error)
}

// New returns the real client if token is non-empty, otherwise the dev
// mock - this is the *only* switch anywhere in this codebase. Once a real
// COC_API_TOKEN exists, nothing else needs to change; there is no separate
// mode flag to also flip. See main.go for where token comes from.
func New(token string) Client {
	if token == "" {
		return mockClient{}
	}
	return &realClient{token: token, http: &http.Client{}}
}

// normalizeTag upper-cases and adds the leading # a tag needs if the caller
// left it off - "p2 abc" and "#P2ABC" should look up the same thing.
func normalizeTag(tag string) string {
	tag = strings.ToUpper(strings.TrimSpace(tag))
	if tag != "" && !strings.HasPrefix(tag, "#") {
		tag = "#" + tag
	}
	return tag
}

const apiBase = "https://api.clashofclans.com/v1"

type realClient struct {
	token string
	http  *http.Client
}

func (c *realClient) Player(ctx context.Context, tag string) (Player, error) {
	var raw struct {
		Tag                      string `json:"tag"`
		Name                     string `json:"name"`
		TownHallLevel            int    `json:"townHallLevel"`
		Trophies                 int    `json:"trophies"`
		ClanCapitalContributions int    `json:"clanCapitalContributions"`
		Heroes                   []struct {
			Name     string `json:"name"`
			Level    int    `json:"level"`
			MaxLevel int    `json:"maxLevel"`
			Village  string `json:"village"`
		} `json:"heroes"`
	}
	if err := c.get(ctx, "/players/"+url.PathEscape(normalizeTag(tag)), &raw); err != nil {
		return Player{}, err
	}
	p := Player{
		Tag: raw.Tag, Name: raw.Name, TownHallLevel: raw.TownHallLevel,
		Trophies: raw.Trophies, CapitalContribution: raw.ClanCapitalContributions,
	}
	for _, h := range raw.Heroes {
		if h.Village == "" || h.Village == "home" {
			p.Heroes = append(p.Heroes, Hero{Name: h.Name, Level: h.Level, Max: h.MaxLevel})
		}
	}
	return p, nil
}

func (c *realClient) Clan(ctx context.Context, tag string) (Clan, error) {
	encodedTag := url.PathEscape(normalizeTag(tag))

	var raw struct {
		Tag               string `json:"tag"`
		Name              string `json:"name"`
		ClanLevel         int    `json:"clanLevel"`
		ClanPoints        int    `json:"clanPoints"`
		ClanCapitalPoints int    `json:"clanCapitalPoints"`
		MemberList        []struct {
			Tag      string `json:"tag"`
			Name     string `json:"name"`
			Role     string `json:"role"`
			ExpLevel int    `json:"expLevel"`
			Trophies int    `json:"trophies"`
		} `json:"memberList"`
	}
	if err := c.get(ctx, "/clans/"+encodedTag, &raw); err != nil {
		return Clan{}, err
	}
	cl := Clan{Tag: raw.Tag, Name: raw.Name, Level: raw.ClanLevel, Points: raw.ClanPoints, CapitalPoints: raw.ClanCapitalPoints}
	for _, m := range raw.MemberList {
		cl.Members = append(cl.Members, ClanMember{Tag: m.Tag, Name: m.Name, Role: m.Role, Level: m.ExpLevel, Trophies: m.Trophies})
	}

	// Best-effort: a clan too new to have Raid Weekend history, or with it
	// disabled, is not a failure - just an empty section on the Lookup page.
	var raids struct {
		Items []struct {
			EndTime          string `json:"endTime"`
			CapitalTotalLoot int    `json:"capitalTotalLoot"`
			OffensiveReward  int    `json:"offensiveReward"`
			Members          []struct {
				Name                   string `json:"name"`
				CapitalResourcesLooted int    `json:"capitalResourcesLooted"`
				Attacks                int    `json:"attacks"`
			} `json:"members"`
		} `json:"items"`
	}
	if err := c.get(ctx, "/clans/"+encodedTag+"/capitalraidseasons?limit=3", &raids); err == nil {
		for _, s := range raids.Items {
			season := RaidSeason{EndTime: s.EndTime, CapitalTotalLoot: s.CapitalTotalLoot, OffensiveReward: s.OffensiveReward}
			for _, m := range s.Members {
				season.Members = append(season.Members, RaidMember{
					Name: m.Name, CapitalResourcesLooted: m.CapitalResourcesLooted, Attacks: m.Attacks,
				})
			}
			cl.RaidSeasons = append(cl.RaidSeasons, season)
		}
	}
	return cl, nil
}

// get decodes a GET response into v, surfacing Supercell's own error body
// on a non-200 rather than a generic message - the API's IP-allowlist
// rejection and similar need to reach whoever is holding the token plainly,
// not get swallowed into "request failed."
func (c *realClient) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("clash of clans api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("clash of clans api %s: %d %s", path, resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// mockClient returns canned, clearly-marked sample data - see Player.Mock
// and Clan.Mock. Used whenever New is called with no token, which is the
// only way to reach it; there is no toggle that could leave this active
// alongside a real token by mistake.
type mockClient struct{}

func (mockClient) Player(ctx context.Context, tag string) (Player, error) {
	return Player{
		Tag: normalizeTag(tag), Name: "Sample Villager", TownHallLevel: 14, Trophies: 3421,
		Heroes: []Hero{
			{Name: "Barbarian King", Level: 75, Max: 80},
			{Name: "Archer Queen", Level: 78, Max: 80},
			{Name: "Grand Warden", Level: 55, Max: 65},
			{Name: "Royal Champion", Level: 25, Max: 30},
		},
		CapitalContribution: 284500,
		Mock:                true,
	}, nil
}

func (mockClient) Clan(ctx context.Context, tag string) (Clan, error) {
	return Clan{
		Tag: normalizeTag(tag), Name: "Sample Clan", Level: 12, Points: 42000, CapitalPoints: 5600,
		Members: []ClanMember{
			{Tag: "#SAMPLE01", Name: "Sample Leader", Role: "leader", Level: 210, Trophies: 4800},
			{Tag: "#SAMPLE02", Name: "Sample Co-Leader", Role: "coLeader", Level: 185, Trophies: 4200},
			{Tag: "#SAMPLE03", Name: "Sample Member", Role: "member", Level: 150, Trophies: 3200},
		},
		RaidSeasons: []RaidSeason{
			{
				EndTime: "20260726T070000.000Z", CapitalTotalLoot: 850000, OffensiveReward: 180,
				Members: []RaidMember{
					{Name: "Sample Leader", CapitalResourcesLooted: 62000, Attacks: 6},
					{Name: "Sample Co-Leader", CapitalResourcesLooted: 54000, Attacks: 6},
				},
			},
			{
				EndTime: "20260719T070000.000Z", CapitalTotalLoot: 790000, OffensiveReward: 170,
				Members: []RaidMember{
					{Name: "Sample Leader", CapitalResourcesLooted: 58000, Attacks: 6},
				},
			},
		},
		Mock: true,
	}, nil
}
