// Package changelog keeps a Keep a Changelog file's Unreleased section to the
// changes no release carries yet.
//
// Auto Release tags a release from every releasable merge, and the entry the
// merged pull request added under Unreleased is part of that release: the
// CHANGELOG.md of the tag lists it under Unreleased. Cut moves every such entry
// into the section of the release that first carried it; Check lists them.
//
// Only Unreleased and the sections receiving entries are rewritten: every other
// section, the preamble and the link references keep their bytes.
package changelog

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Unreleased is the name of the section holding the changes no release carries.
const Unreleased = "Unreleased"

// groupOrder is Keep a Changelog's order of change types within a version.
var groupOrder = []string{"Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"}

var (
	linkRefPattern = regexp.MustCompile(`^\[[^\]]+\]: `)
	// The repository a link reference points at, from its compare or tree URL.
	repoURLPattern = regexp.MustCompile(`^\[` + Unreleased + `\]: (https://[^/]+/[^/]+/[^/]+)/`)
)

// Release is one release as cut: the tags on one commit are one release, named
// by the highest version among them (a promoted release candidate by its final
// version).
type Release struct {
	// Version is the release's version without the tag's "v".
	Version *semver.Version
	// Tag is the tag the release is named by.
	Tag string
	// Date is the tagged commit's date, YYYY-MM-DD.
	Date string
	// Changelog is the CHANGELOG.md at the tag; empty when the tag has none.
	Changelog string
}

// Moved is an entry Cut moved out of Unreleased.
type Moved struct {
	// Release is the tag of the release that carried the entry first.
	Release string
	// Entry is the entry's text, its bullet line and the lines continuing it.
	Entry string
}

// Summary is the entry's first line, at most n runes.
func (m Moved) Summary(n int) string {
	s, _, _ := strings.Cut(m.Entry, "\n")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

type document struct {
	preamble []string
	sections []*section
	links    []string // the trailing link references and the blank lines among them
}

type section struct {
	heading string   // the "## " line as written
	name    string   // "Unreleased" or a version without its "v"
	body    []string // the lines below the heading as written
	tag     string   // the release's tag, set when Cut created the section
	groups  []*group // parsed body, set once the section is rewritten
}

type group struct {
	name    string // "Added", "Changed", …; empty for entries before any group heading
	entries []string
}

// HasUnreleased reports whether text has an Unreleased section to cut.
func HasUnreleased(text string) bool {
	return parse(text).find(Unreleased) != nil
}

// Cut moves every Unreleased entry a release already carried into that
// release's section, creating the section (and its link reference, when the
// file keeps them) where it is missing, and returns the new text and the moved
// entries. A text without an Unreleased section is returned unchanged. An entry
// added again after a release carried it once stays: a release is credited
// only with the copies its CHANGELOG.md holds beyond the previous release's.
func Cut(text string, releases []Release) (string, []Moved) {
	doc := parse(text)
	unrel := doc.find(Unreleased)
	if unrel == nil {
		return text, nil
	}
	releases = sorted(releases)
	unrel.parseGroups()

	// credit[key] lists, oldest first, the release of every copy of an entry
	// the releases carried under Unreleased.
	credit := map[string][]Release{}
	prev := map[string]int{}
	for _, r := range releases {
		counts := map[string]int{}
		if s := parse(r.Changelog).find(Unreleased); s != nil {
			s.parseGroups()
			for _, g := range s.groups {
				for _, e := range g.entries {
					counts[key(e)]++
				}
			}
		}
		for k, n := range counts {
			for i := prev[k]; i < n; i++ {
				credit[k] = append(credit[k], r)
			}
		}
		prev = counts
	}

	var moved []Moved
	for _, g := range unrel.groups {
		kept := g.entries[:0]
		for _, e := range g.entries {
			k := key(e)
			if len(credit[k]) == 0 {
				kept = append(kept, e)
				continue
			}
			r := credit[k][0]
			credit[k] = credit[k][1:]
			target := doc.release(r).group(g.name)
			target.entries = append(target.entries, e)
			moved = append(moved, Moved{Release: r.Tag, Entry: e})
		}
		g.entries = kept
	}
	if len(moved) == 0 {
		return text, nil
	}
	doc.link()
	return doc.render(), moved
}

// Pending splits the entries Cut moves out of text into the ones base already
// held released under Unreleased, which wait for the cut of base itself, and
// the rest, which text brings on its own. A pull request is checked on its
// merge with base: a release cut on base after base's last cut makes every
// pull request carry base's released entries until base's cut lands, and
// those are not the pull request's to clear.
func Pending(text, base string, releases []Release) (own, pending []Moved) {
	_, moved := Cut(text, releases)
	_, onBase := Cut(base, releases)
	left := map[Moved]int{}
	for _, m := range onBase {
		left[m]++
	}
	for _, m := range moved {
		if left[m] > 0 {
			left[m]--
			pending = append(pending, m)
			continue
		}
		own = append(own, m)
	}
	return own, pending
}

// key is an entry's identity: its lines without trailing white space.
func key(entry string) string {
	lines := strings.Split(entry, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func sorted(releases []Release) []Release {
	out := append([]Release(nil), releases...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Version.LessThan(out[j].Version) })
	return out
}

func parse(text string) *document {
	doc := &document{}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}

	// The link references close the file; the blank lines among them belong
	// to them too.
	end := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		if linkRefPattern.MatchString(lines[i]) {
			end = i
		} else if strings.TrimSpace(lines[i]) != "" {
			break
		}
	}
	doc.links = lines[end:]
	lines = lines[:end]

	var sec *section
	for _, l := range lines {
		if strings.HasPrefix(l, "## ") {
			sec = &section{heading: l, name: headingName(l)}
			doc.sections = append(doc.sections, sec)
			continue
		}
		if sec == nil {
			doc.preamble = append(doc.preamble, l)
			continue
		}
		sec.body = append(sec.body, l)
	}
	return doc
}

// headingName reads "## [2.5.0] - 2026-10-17", "## 2.5.0" or "## [Unreleased]".
func headingName(line string) string {
	name, _, _ := strings.Cut(strings.TrimPrefix(line, "## "), " - ")
	return strings.Trim(strings.TrimSpace(name), "[]")
}

func (d *document) find(name string) *section {
	for _, s := range d.sections {
		if s.name == name {
			return s
		}
	}
	return nil
}

// release returns the section of r, inserted in version order when it is
// missing.
func (d *document) release(r Release) *section {
	name := r.Version.Original()
	if s := d.find(name); s != nil {
		s.parseGroups()
		return s
	}
	s := &section{heading: fmt.Sprintf("## [%s] - %s", name, r.Date), name: name, tag: r.Tag}
	s.parseGroups()

	at := len(d.sections)
	for i, o := range d.sections {
		if o.name == Unreleased {
			continue
		}
		if v, err := semver.NewVersion(o.name); err == nil && v.LessThan(r.Version) {
			at = i
			break
		}
	}
	d.sections = append(d.sections[:at], append([]*section{s}, d.sections[at:]...)...)
	return s
}

// link adds the link reference of every section Cut created, comparing it with
// the next older section, and points Unreleased's compare at the newest
// release, when the file keeps link references.
func (d *document) link() {
	repo := ""
	unrelLink := -1
	for i, l := range d.links {
		if m := repoURLPattern.FindStringSubmatch(l); m != nil {
			repo, unrelLink = m[1], i
		}
	}
	if repo == "" {
		return
	}

	// Oldest first, so the next older section's reference is in place.
	for i := len(d.sections) - 1; i >= 0; i-- {
		s := d.sections[i]
		if s.tag == "" {
			continue
		}
		ref := fmt.Sprintf("[%s]: %s/releases/tag/%s", s.name, repo, s.tag)
		insert := len(d.links)
		if older := d.older(i); older != nil {
			ref = fmt.Sprintf("[%s]: %s/compare/%s...%s", s.name, repo, d.tagOf(older), s.tag)
			for j, l := range d.links {
				if strings.HasPrefix(l, "["+older.name+"]: ") {
					insert = j
					break
				}
			}
		}
		d.links = append(d.links[:insert], append([]string{ref}, d.links[insert:]...)...)
		if insert <= unrelLink {
			unrelLink++
		}
	}

	if newest := d.older(-1); newest != nil {
		d.links[unrelLink] = fmt.Sprintf("[%s]: %s/compare/%s...HEAD", Unreleased, repo, d.tagOf(newest))
	}
}

// older is the first release section after index i.
func (d *document) older(i int) *section {
	for _, o := range d.sections[i+1:] {
		if o.name != Unreleased {
			return o
		}
	}
	return nil
}

// tagOf is the tag of a section's release: the one it was created for, else
// by the file's own link references, "v" unless they compare tags without one.
func (d *document) tagOf(s *section) string {
	if s.tag != "" {
		return s.tag
	}
	name := s.name
	for _, l := range d.links {
		if strings.HasSuffix(l, "..."+name) {
			return name
		}
	}
	return "v" + name
}

// parseGroups reads the section's body into groups of entries. An entry is a
// bullet line with every line continuing it, blank lines and fenced blocks
// included.
func (s *section) parseGroups() {
	if s.groups != nil {
		return
	}
	s.groups = []*group{}
	var g *group
	fenced := false
	for _, l := range s.body {
		switch {
		case fenced:
			g.entries[len(g.entries)-1] += "\n" + l
		case strings.HasPrefix(l, "### "):
			g = s.group(strings.TrimSpace(strings.TrimPrefix(l, "### ")))
			continue
		case strings.TrimSpace(l) == "" && (g == nil || len(g.entries) == 0):
		case strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* ") || g == nil || len(g.entries) == 0:
			if g == nil {
				g = s.group("")
			}
			g.entries = append(g.entries, l)
		default:
			g.entries[len(g.entries)-1] += "\n" + l
		}
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			fenced = !fenced
		}
	}
	for _, g := range s.groups {
		for i, e := range g.entries {
			g.entries[i] = key(e)
		}
	}
}

func (s *section) group(name string) *group {
	for _, g := range s.groups {
		if g.name == name {
			return g
		}
	}
	g := &group{name: name}
	s.groups = append(s.groups, g)
	sort.SliceStable(s.groups, func(i, j int) bool { return groupRank(s.groups[i].name) < groupRank(s.groups[j].name) })
	return g
}

func groupRank(name string) int {
	if name == "" {
		return -1
	}
	for i, n := range groupOrder {
		if n == name {
			return i
		}
	}
	return len(groupOrder)
}

func (d *document) render() string {
	var b strings.Builder
	for _, l := range d.preamble {
		b.WriteString(l + "\n")
	}
	for _, s := range d.sections {
		b.WriteString(s.heading + "\n")
		if s.groups == nil {
			for _, l := range s.body {
				b.WriteString(l + "\n")
			}
			continue
		}
		b.WriteString("\n")
		for _, g := range s.groups {
			if len(g.entries) == 0 {
				continue
			}
			if g.name != "" {
				b.WriteString("### " + g.name + "\n\n")
			}
			for _, e := range g.entries {
				b.WriteString(e + "\n")
			}
			b.WriteString("\n")
		}
	}
	for _, l := range d.links {
		b.WriteString(l + "\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
