// Package graph computes kinship (cousin degree / removal) over parent links.
package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/richardwooding/kin/internal/model"
)

// Ancestors returns every ancestor of id mapped to the smallest generation distance.
func Ancestors(g *model.Graph, id string) map[string]int {
	dist := map[string]int{id: 0}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		p := g.Persons[cur]
		if p == nil {
			continue
		}
		for _, par := range []string{p.Father, p.Mother} {
			if par == "" {
				continue
			}
			if _, seen := dist[par]; !seen {
				dist[par] = dist[cur] + 1
				queue = append(queue, par)
			}
		}
	}
	return dist
}

// Relation describes how b relates to a through their nearest common ancestors.
type Relation struct {
	Up     int      `json:"up"`   // generations from a up to the common ancestor
	Down   int      `json:"down"` // generations from the common ancestor down to b
	Common []string `json:"common"`
	Label  string   `json:"label"`
}

// Relationship finds the nearest common ancestor(s) of a and b: the shared
// ancestors with the smallest total generation distance, and among equal
// totals the ones nearest to a. The result is deterministic for a given graph.
func Relationship(g *model.Graph, a, b string) (*Relation, bool) {
	aa := Ancestors(g, a)
	bb := Ancestors(g, b)
	shared := make([]string, 0, 8)
	for id := range aa {
		if _, ok := bb[id]; ok {
			shared = append(shared, id)
		}
	}
	if len(shared) == 0 {
		return nil, false
	}
	sort.Strings(shared)
	var rel *Relation
	for _, id := range shared {
		ua, ub := aa[id], bb[id]
		switch {
		case rel == nil || ua+ub < rel.Up+rel.Down || (ua+ub == rel.Up+rel.Down && ua < rel.Up):
			rel = &Relation{Up: ua, Down: ub, Common: []string{id}}
		case ua+ub == rel.Up+rel.Down && ua == rel.Up:
			rel.Common = append(rel.Common, id)
		}
	}
	rel.Label = Label(rel.Up, rel.Down)
	return rel, true
}

// Label names the relationship of b to a for the given generation counts.
func Label(up, down int) string {
	switch {
	case up == 0 && down == 0:
		return "same person"
	case up == 0:
		return greats(down-1) + "child" // b is a's descendant
	case down == 0:
		return greats(up-1) + "parent"
	case up == 1 && down == 1:
		return "sibling (or half-sibling)"
	case up == 1:
		return greats(down-2) + "niece/nephew"
	case down == 1:
		return greats(up-2) + "aunt/uncle"
	}
	deg := min(up, down) - 1
	rem := abs(up - down)
	s := fmt.Sprintf("%s cousin", ordinal(deg))
	if rem > 0 {
		s += fmt.Sprintf(", %d× removed", rem)
	}
	return s
}

func greats(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "grand"
	}
	return strings.Repeat("great-", n-1) + "grand"
}

func ordinal(n int) string {
	suffix := "th"
	switch n % 10 {
	case 1:
		if n%100 != 11 {
			suffix = "st"
		}
	case 2:
		if n%100 != 12 {
			suffix = "nd"
		}
	case 3:
		if n%100 != 13 {
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Components labels connected components (parent + spouse edges) and returns id -> component index.
func Components(g *model.Graph) map[string]int {
	adj := map[string][]string{}
	link := func(a, b string) {
		if a == "" || b == "" {
			return
		}
		adj[a] = append(adj[a], b)
		adj[b] = append(adj[b], a)
	}
	for id, p := range g.Persons {
		link(id, p.Father)
		link(id, p.Mother)
		for _, s := range p.Spouses {
			link(id, s)
		}
	}
	comp := map[string]int{}
	n := 0
	ids := make([]string, 0, len(g.Persons))
	for id := range g.Persons {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, ok := comp[id]; ok {
			continue
		}
		stack := []string{id}
		comp[id] = n
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, nb := range adj[cur] {
				if _, ok := comp[nb]; !ok {
					comp[nb] = n
					stack = append(stack, nb)
				}
			}
		}
		n++
	}
	return comp
}
