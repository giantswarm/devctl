package reposetup

import (
	"context"
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

const (
	// DefaultOwner is the GitHub organisation repositories are created in.
	DefaultOwner = "giantswarm"
	// FallbackTeam is the CODEOWNERS fallback owner of giantswarm/github; its
	// members may add entries to any team file without the team's review.
	FallbackTeam = "team-planeteers"
	// MaxMachineApprovedEntries is how many entries a creation-only pull
	// request may add and still be approved by the machine; more is a
	// migration and gets a person.
	MaxMachineApprovedEntries = 3
)

// Validator validates the entries of a team file that are about to create
// repositories and returns the dry-run value.
type Validator struct {
	// Schema the entries are validated against. Required.
	Schema *Schema
	// Names checks whether a repository name is free on GitHub. Optional:
	// without one every name is reported unchecked and the result says so.
	Names NameChecker
	// Owner is the GitHub organisation; DefaultOwner when empty.
	Owner string
}

// Request is one validation: the team file, the entries being added and the
// author of the change.
type Request struct {
	// TeamFile the entries live in; its team is the owning team. Required.
	TeamFile *TeamFile
	// Names of the entries being added, the ones the creation rules apply
	// to. Nil means every entry of the file.
	Names []string
	// Author is the GitHub login of the person opening the change; empty
	// when unknown, which skips the team guard.
	Author string
	// AuthorTeams are the GitHub team slugs the author is a member of
	// (team-bumblebee; a giantswarm/ or @giantswarm/ prefix is accepted).
	AuthorTeams []string
}

// Result is the dry-run value: what the reconciler would do with the entries
// and why it would refuse. `devctl repo validate` prints it as JSON,
// giantswarm-repo-manager's validate_repository returns it and the validation
// workflow renders it.
type Result struct {
	// Team that owns the entries: the team file's.
	Team string `json:"team"`
	// Schema the entries were validated against.
	Schema SchemaOrigin `json:"schema"`
	// Entries in request order.
	Entries []Entry `json:"entries"`
	// Notices are the guard notices that apply to the change as a whole.
	Notices []Notice `json:"notices,omitempty"`
	// Accepted is true when every entry is accepted.
	Accepted bool `json:"accepted"`
}

// Entry is the dry run of one declaration.
type Entry struct {
	// Name of the repository.
	Name string `json:"name"`
	// Rendered is the entry as it would be written to the team file, defaults
	// applied (gen.ci.generate: true), as a one-item YAML list.
	Rendered string `json:"rendered"`
	// Template the repository would be scaffolded from; empty when the
	// declaration does not derive one.
	Template Template `json:"template,omitempty"`
	// Options the template's scaffold offers, selected by name through
	// [RenderRequest.Options]; nil when the template has none.
	Options []Option `json:"options,omitempty"`
	// NameCheck is the verdict of the GitHub name check.
	NameCheck NameCheck `json:"nameCheck"`
	// Problems are the refusals, each naming the field. Empty when accepted.
	Problems []Problem `json:"problems,omitempty"`
	// Accepted is true when the entry has no problems.
	Accepted bool `json:"accepted"`
}

// Problem is one refusal: the field in dotted form (gen.ci.chartName,
// gen.flavours[1]; "(entry)" for the entry as a whole) and why.
type Problem struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (p Problem) String() string {
	return p.Field + ": " + p.Message
}

// NoticeKind classifies a guard notice.
type NoticeKind string

const (
	// NoticeTeamReview: the author is outside the owning team and the
	// fallback team, so the team's review is required.
	NoticeTeamReview NoticeKind = "team-review"
	// NoticeBatchReview: more than MaxMachineApprovedEntries entries are
	// added, so a person reviews.
	NoticeBatchReview NoticeKind = "batch-review"
	// NoticeNamesUnchecked: no NameChecker was configured.
	NoticeNamesUnchecked NoticeKind = "names-unchecked"
)

// Notice is a guard notice about the change as a whole. A notice does not
// refuse; it tells the author what review the change will get.
type Notice struct {
	Kind    NoticeKind `json:"kind"`
	Message string     `json:"message"`
}

// Validate validates the requested entries and returns the dry-run value.
// A refused entry is data in the result, not an error; an error means the
// validation itself could not run (a requested entry that is not in the
// file, GitHub unreachable).
func (v Validator) Validate(ctx context.Context, req Request) (*Result, error) {
	if v.Schema == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Schema must not be nil", v)
	}
	if req.TeamFile == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.TeamFile must not be nil", req)
	}
	owner := v.Owner
	if owner == "" {
		owner = DefaultOwner
	}

	entries, err := selectEntries(req.TeamFile, req.Names)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	duplicates := duplicateNames(req.TeamFile)

	result := &Result{
		Team:     req.TeamFile.Team,
		Schema:   v.Schema.Origin,
		Entries:  make([]Entry, 0, len(entries)),
		Accepted: true,
	}
	for _, d := range entries {
		entry, err := v.validateEntry(ctx, owner, req.TeamFile, d, duplicates[d.Name])
		if err != nil {
			return nil, microerror.Mask(err)
		}
		result.Entries = append(result.Entries, entry)
		if !entry.Accepted {
			result.Accepted = false
		}
	}
	result.Notices = v.notices(req, len(entries))

	return result, nil
}

// selectEntries picks the named entries in request order, or every entry.
func selectEntries(tf *TeamFile, names []string) ([]Declaration, error) {
	if names == nil {
		return tf.Entries, nil
	}
	entries := make([]Declaration, 0, len(names))
	for _, name := range names {
		d, ok := tf.Entry(name)
		if !ok {
			return nil, microerror.Maskf(entryNotFoundError, "no entry named %q in the team file of %s", name, tf.Team)
		}
		entries = append(entries, d)
	}
	return entries, nil
}

// duplicateNames returns the names that appear more than once in the file.
func duplicateNames(tf *TeamFile) map[string]bool {
	seen := map[string]int{}
	for _, d := range tf.Entries {
		if d.Name != "" {
			seen[d.Name]++
		}
	}
	duplicates := map[string]bool{}
	for name, n := range seen {
		if n > 1 {
			duplicates[name] = true
		}
	}
	return duplicates
}

func (v Validator) validateEntry(ctx context.Context, owner string, tf *TeamFile, d Declaration, duplicate bool) (Entry, error) {
	entry := Entry{
		Name:      d.Name,
		NameCheck: NameCheck{Verdict: VerdictUnchecked, Detail: "not checked"},
	}
	refuse := func(field, format string, args ...any) {
		entry.Problems = append(entry.Problems, Problem{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	// The entry as the team file would carry it, defaults written out: that
	// is what the schema and the rules see, and what is rendered.
	d = withDefaults(d)

	// The schema first: it names type and enum violations and unknown
	// fields, so the rules below can read the fields they need.
	instance, err := d.Instance()
	if err != nil {
		refuse(entryField, "cannot read the entry: %v", err)
	} else {
		entry.Problems = append(entry.Problems, v.Schema.Problems(instance)...)
	}
	if duplicate {
		refuse("name", "%q is declared more than once in the team file of %s", d.Name, tf.Team)
	}

	fields, err := d.fields()
	if err != nil {
		if len(entry.Problems) == 0 {
			refuse(entryField, "cannot read the entry: %v", err)
		}
		fields = entryFields{Name: d.Name}
	}

	// The creation rules: the declaration has to say what to generate.
	var flavours []string
	var language string
	if fields.Gen == nil {
		refuse("gen.flavours", "required for a repository the reconciler creates")
		refuse("gen.language", "required for a repository the reconciler creates")
	} else {
		flavours, language = fields.Gen.Flavours, fields.Gen.Language
		if len(flavours) == 0 {
			refuse("gen.flavours", "required for a repository the reconciler creates")
		}
		if language == "" {
			refuse("gen.language", "required for a repository the reconciler creates")
		}
	}
	known := true
	for i, f := range flavours {
		if _, err := gen.NewFlavour(f); err != nil {
			refuse(fmt.Sprintf("gen.flavours[%d]", i), "must be one of %s", strings.Join(gen.AllFlavours(), "|"))
			known = false
		}
	}
	if language != "" {
		if _, err := gen.NewLanguage(language); err != nil {
			refuse("gen.language", "must be one of %s", strings.Join(gen.AllLanguages(), "|"))
			known = false
		}
	}

	// The template: derived, never declared.
	if known && len(flavours) > 0 && language != "" {
		template, err := DeriveTemplate(fields.ComponentType, flavours, language)
		switch {
		case IsTemplateUnavailable(err):
			refuse("gen.language", "%s", nodeTemplateUnavailable)
		case err != nil:
			return Entry{}, microerror.Mask(err)
		default:
			entry.Template = template
			entry.Options = templateOptions(template)
		}
	}

	// Generated CI needs something to build: align-files' `devctl gen
	// circleci` refuses a declaration with no job, so the dry run does.
	if known && len(flavours) > 0 && language != "" && fields.Gen.CI != nil && fields.Gen.CI.Generate != nil && *fields.Gen.CI.Generate && !hasCIJob(fields) {
		refuse("gen.ci.generate", "no CircleCI job for language %s without the app flavour or gen.ci.image.dockerfile; set it to false", language)
	}

	// The name: lowercase, the chart's name where a chart exists, free on
	// GitHub. A name the schema already refused is not checked on GitHub.
	nameValid := d.Name != ""
	if d.Name != "" {
		switch {
		case hasChart(flavours) && !chartNamePattern.MatchString(d.Name):
			refuse("name", "%s", chartNameRule)
			nameValid = false
		case !repositoryNamePattern.MatchString(d.Name):
			refuse("name", "%s", repositoryNameRule)
			nameValid = false
		}
		if hasChart(flavours) {
			if strings.HasSuffix(d.Name, chartSuffix) {
				refuse("name", "a chart repository is named after its chart, without the %s suffix", chartSuffix)
			}
			if fields.Gen != nil && fields.Gen.CI != nil && fields.Gen.CI.ChartName != "" && fields.Gen.CI.ChartName != d.Name {
				refuse("gen.ci.chartName", "must equal the repository name %q: the repository is named after its chart", d.Name)
			}
		}
	}
	if nameValid && v.Names == nil {
		entry.NameCheck = NameCheck{Verdict: VerdictUnchecked, Detail: "not checked: no GitHub client"}
	}
	if nameValid && v.Names != nil {
		check, err := v.Names.CheckName(ctx, owner, d.Name)
		if err != nil {
			return Entry{}, microerror.Mask(err)
		}
		entry.NameCheck = check
		if check.Verdict == VerdictTaken {
			refuse("name", "taken: %s", check.Detail)
		}
	}

	rendered, err := d.YAML()
	if err != nil {
		return Entry{}, microerror.Mask(err)
	}
	entry.Rendered = rendered
	entry.Accepted = len(entry.Problems) == 0

	return entry, nil
}

// withDefaults returns a copy of the declaration with the creation defaults
// written out: gen.ci.generate is true unless the entry says otherwise. The
// team file's node is left as it was read.
func withDefaults(d Declaration) Declaration {
	node := cloneNode(d.node)

	if genNode := mappingValue(node, "gen"); genNode != nil && genNode.Kind == yaml.MappingNode {
		ci := mappingValue(genNode, "ci")
		if ci == nil {
			ci = mappingNode()
			setMappingValue(genNode, "ci", ci)
		}
		if ci.Kind == yaml.MappingNode && mappingValue(ci, "generate") == nil {
			ci.Content = append([]*yaml.Node{scalarNode("generate"), {Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}}, ci.Content...)
		}
	}

	return Declaration{Name: d.Name, node: node}
}

// notices are the guards on the change as a whole.
func (v Validator) notices(req Request, added int) []Notice {
	var notices []Notice

	team := req.TeamFile.Team
	if req.Author != "" && !memberOf(req.AuthorTeams, team) && !memberOf(req.AuthorTeams, FallbackTeam) {
		notices = append(notices, Notice{
			Kind:    NoticeTeamReview,
			Message: fmt.Sprintf("your team's review will be required: %s is not a member of %s or %s, so the machine does not approve the pull request", req.Author, team, FallbackTeam),
		})
	}
	if added > MaxMachineApprovedEntries {
		notices = append(notices, Notice{
			Kind:    NoticeBatchReview,
			Message: fmt.Sprintf("a person will review: %d entries are added and the machine approves at most %d", added, MaxMachineApprovedEntries),
		})
	}
	if v.Names == nil {
		notices = append(notices, Notice{
			Kind:    NoticeNamesUnchecked,
			Message: "repository names were not checked on GitHub: no GitHub client was configured",
		})
	}

	return notices
}

// memberOf says whether a team slug is in the list; the slugs may carry the
// organisation prefix CODEOWNERS uses (@giantswarm/team-bumblebee).
func memberOf(teams []string, team string) bool {
	for _, t := range teams {
		t = strings.TrimPrefix(strings.TrimPrefix(t, "@"), DefaultOwner+"/")
		if strings.EqualFold(t, team) {
			return true
		}
	}
	return false
}

// hasCIJob says whether the CircleCI generator has a job for the
// declaration: a Go or Node build, an image (a scaffold has a Dockerfile only
// where gen.ci.image.dockerfile names one) or a chart.
func hasCIJob(f entryFields) bool {
	g := f.Gen
	switch {
	case g.Language == gen.LanguageGo.String(), g.Language == gen.LanguageNode.String():
		return true
	case g.CI != nil && g.CI.Image != nil && g.CI.Image.Dockerfile != "":
		return true
	}
	for _, flavour := range g.Flavours {
		if flavour == gen.FlavourApp.String() {
			return true
		}
	}
	return false
}
