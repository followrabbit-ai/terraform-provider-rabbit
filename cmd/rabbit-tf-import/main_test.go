package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/followrabbit-ai/terraform-provider-rabbit/internal/client"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Platform Admins":            "platform_admins",
		"Domain admins":              "domain_admins",
		" my-team@acme.com ":         "my_team_acme_com",
		"7day Group":                 "_7day_group",
		"!!!":                        "g",
		"DATA TEAM (prod) — viewers": "data_team_prod_viewers",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q want %q", in, got, want)
		}
	}
}

func TestEmit_groupsOnly(t *testing.T) {
	groups := []client.Group{
		{ID: "g1", Name: "Platform Admins"},
		{ID: "g2", Name: "Platform Admins"}, // collision
		{ID: "g3", Name: "Viewers"},
	}
	var buf bytes.Buffer
	emit(&buf, "acme.com", "", groups, false)
	out := buf.String()

	for _, want := range []string{
		`to = rabbit_group.platform_admins`,
		`id = "acme.com/g1"`,
		`to = rabbit_group.platform_admins_2`,
		`id = "acme.com/g2"`,
		`to = rabbit_group.viewers`,
		`id = "acme.com/g3"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "rabbit_group_member") {
		t.Errorf("members should not appear when includeMembers=false:\n%s", out)
	}
}

func TestEmit_includeMembers(t *testing.T) {
	groups := []client.Group{
		{
			ID:   "g1",
			Name: "Admins",
			Principals: []client.Principal{
				{Name: "alice@acme.com", PrincipalType: client.PrincipalEmail},
				{Name: "bob@acme.com", PrincipalType: client.PrincipalEmail},
			},
		},
	}
	var buf bytes.Buffer
	emit(&buf, "acme.com", "", groups, true)
	out := buf.String()

	for _, want := range []string{
		`to = rabbit_group.admins`,
		`id = "acme.com/g1"`,
		`to = rabbit_group_member.admins_alice_acme_com`,
		`id = "acme.com/g1/EMAIL/alice@acme.com"`,
		`to = rabbit_group_member.admins_bob_acme_com`,
		`id = "acme.com/g1/EMAIL/bob@acme.com"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestEmit_resourcePrefix(t *testing.T) {
	groups := []client.Group{{ID: "g1", Name: "Viewers"}}
	var buf bytes.Buffer
	emit(&buf, "acme.com", "acme_", groups, false)
	if !strings.Contains(buf.String(), "rabbit_group.acme_viewers") {
		t.Errorf("expected prefixed address, got:\n%s", buf.String())
	}
}
