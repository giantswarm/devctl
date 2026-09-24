package reposetup

import (
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// Template is what a created repository is scaffolded from: a template
// repository of the giantswarm organisation, or the minimal scaffold.
type Template string

const (
	// TemplateGo is giantswarm/template, the Go service and CLI template.
	TemplateGo Template = "giantswarm/template"
	// TemplateChart is giantswarm/template-app, the chart-only template.
	TemplateChart Template = "giantswarm/template-app"
	// TemplatePlans is giantswarm/template-plans, the team plans template:
	// versioned PRDs, their companion websites and the plan-workflow agent
	// skills.
	TemplatePlans Template = "giantswarm/template-plans"
	// TemplateMinimal is the minimal scaffold: README, LICENSE, DCO,
	// SECURITY.md, CODEOWNERS and .gitignore plus the generated files.
	TemplateMinimal Template = "minimal"
)

// Repository is the owner/name of the template repository, or empty for the
// minimal scaffold.
func (t Template) Repository() string {
	if t == TemplateMinimal {
		return ""
	}
	return string(t)
}

func (t Template) String() string {
	return string(t)
}

// shipsReadme says whether t carries its own repository-specific README,
// which the scaffold must keep rather than overwrite with the generic stub:
// giantswarm/template-app and giantswarm/template-plans both ship the real
// README of the repository they scaffold (placeholders included, so
// replacePlaceholders leaves it correct), unlike giantswarm/template, whose
// README describes the Go template itself and so is replaced like the
// minimal scaffold's.
func (t Template) shipsReadme() bool {
	return t == TemplateChart || t == TemplatePlans
}

// componentTypeCustomer is the component type of a customer repository.
const componentTypeCustomer = "customer"

// nodeTemplateUnavailable is why language node is refused: the Node
// template is not part of this release.
const nodeTemplateUnavailable = "the Node template is not available yet"

// plansTemplateLanguageUnavailable is why the plans flavour is refused with
// a language other than generic: giantswarm/template-plans has one shape, a
// generic repository, and no per-language variant.
const plansTemplateLanguageUnavailable = "the plans flavour derives giantswarm/template-plans, which is generic only: set gen.language to generic"

// DeriveTemplate returns the template a declaration is scaffolded from.
// There is no template field: the component type, flavours and language
// decide. Language go → [TemplateGo]; the plans flavour with language
// generic → [TemplatePlans], refused for any other language with an error
// [IsTemplateUnavailable] asserts; language generic with the app flavour →
// [TemplateChart]; the customer flavour or component type, the fork
// flavour, the languages python and kyverno-policy, and a generic repository
// without a chart → [TemplateMinimal]. Language node has no template yet and is refused with
// an error [IsTemplateUnavailable] asserts. An unknown flavour or language
// is refused with the error [gen.NewFlavour] or [gen.NewLanguage] returns.
func DeriveTemplate(componentType string, flavours []string, language string) (Template, error) {
	lang, err := gen.NewLanguage(language)
	if err != nil {
		return "", microerror.Mask(err)
	}

	var fl gen.FlavourSlice
	for _, f := range flavours {
		flavour, err := gen.NewFlavour(f)
		if err != nil {
			return "", microerror.Mask(err)
		}
		fl = append(fl, flavour)
	}

	if componentType == componentTypeCustomer || fl.Contains(gen.FlavourCustomer) || !fl.Generates() {
		return TemplateMinimal, nil
	}

	// The plans flavour wins over the language switch below (and so over
	// its generic → minimal fallthrough), the same way customer and fork
	// already won above: it names its own template, not a language's.
	if fl.Contains(gen.FlavourPlans) {
		if lang != gen.LanguageGeneric {
			return "", microerror.Maskf(templateUnavailableError, "%s", plansTemplateLanguageUnavailable)
		}
		return TemplatePlans, nil
	}

	switch lang {
	case gen.LanguageGo:
		return TemplateGo, nil
	case gen.LanguageNode:
		return "", microerror.Maskf(templateUnavailableError, "%s", nodeTemplateUnavailable)
	case gen.LanguageGeneric:
		if fl.Contains(gen.FlavourApp) {
			return TemplateChart, nil
		}
		return TemplateMinimal, nil
	default:
		return TemplateMinimal, nil
	}
}

// unavailableTemplateReason says which [templateUnavailableError] message
// [DeriveTemplate] would have refused flavours and language with -- the same
// precedence, the plans flavour first: a declaration's own message, not the
// other's, when a caller already knows [IsTemplateUnavailable] holds and
// wants the reason without re-deriving.
func unavailableTemplateReason(flavours []string, language string) string {
	for _, f := range flavours {
		if f == gen.FlavourPlans.String() && language != gen.LanguageGeneric.String() {
			return plansTemplateLanguageUnavailable
		}
	}
	return nodeTemplateUnavailable
}

// HasChart says whether the declared flavours produce a Helm chart, which
// is where the chart-name convention applies.
func HasChart(flavours []string) bool {
	for _, f := range flavours {
		if f == gen.FlavourApp.String() || f == gen.FlavourClusterApp.String() {
			return true
		}
	}
	return false
}

// DeriveChart returns the template whose chart the scaffold carries beside
// the derived template t: [TemplateChart] when the flavours produce a chart
// ([HasChart]) and t is not the chart template already -- the Go service
// with the app flavour is the Go template plus the chart template's chart at
// helm/<name>. Empty when the flavours produce no chart or t carries it.
func DeriveChart(t Template, flavours []string) Template {
	if t == TemplateChart || !HasChart(flavours) {
		return ""
	}
	return TemplateChart
}
