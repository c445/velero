/*
Copyright The Velero Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package collections

import (
	"strings"

	"github.com/gobwas/glob"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/vmware-tanzu/velero/pkg/discovery"
)

type globStringSet struct {
	sets.String
}

func newGlobStringSet() globStringSet {
	return globStringSet{sets.NewString()}
}

func (gss globStringSet) match(match string) bool {
	for _, item := range gss.List() {
		g, err := glob.Compile(item)
		if err != nil {
			return false
		}
		if g.Match(match) {
			return true
		}
	}
	return false
}

// IncludesExcludes is a type that manages lists of included
// and excluded items. The logic implemented is that everything
// in the included list except those items in the excluded list
// should be included. '*' in the includes list means "include
// everything", but it is not valid in the exclude list.
type IncludesExcludes struct {
	includes globStringSet
	excludes globStringSet
}

func NewIncludesExcludes() *IncludesExcludes {
	return &IncludesExcludes{
		includes: newGlobStringSet(),
		excludes: newGlobStringSet(),
	}
}

// Includes adds items to the includes list. '*' is a wildcard
// value meaning "include everything".
func (ie *IncludesExcludes) Includes(includes ...string) *IncludesExcludes {
	ie.includes.Insert(includes...)
	return ie
}

// GetIncludes returns the items in the includes list
func (ie *IncludesExcludes) GetIncludes() []string {
	return ie.includes.List()
}

// Excludes adds items to the excludes list
func (ie *IncludesExcludes) Excludes(excludes ...string) *IncludesExcludes {
	ie.excludes.Insert(excludes...)
	return ie
}

// GetExcludes returns the items in the excludes list
func (ie *IncludesExcludes) GetExcludes() []string {
	return ie.excludes.List()
}

// ShouldInclude returns whether the specified item should be
// included or not. Everything in the includes list except those
// items in the excludes list should be included.
func (ie *IncludesExcludes) ShouldInclude(s string) bool {
	if ie.excludes.match(s) {
		return false
	}

	// len=0 means include everything
	return ie.includes.Len() == 0 || ie.includes.Has("*") || ie.includes.match(s)
}

// IncludesString returns a string containing all of the includes, separated by commas, or * if the
// list is empty.
func (ie *IncludesExcludes) IncludesString() string {
	return asString(ie.GetIncludes(), "*")
}

// ExcludesString returns a string containing all of the excludes, separated by commas, or <none> if the
// list is empty.
func (ie *IncludesExcludes) ExcludesString() string {
	return asString(ie.GetExcludes(), "<none>")
}

func asString(in []string, empty string) string {
	if len(in) == 0 {
		return empty
	}
	return strings.Join(in, ", ")
}

// IncludeEverything returns true if the includes list is empty or '*'
// and the excludes list is empty, or false otherwise.
func (ie *IncludesExcludes) IncludeEverything() bool {
	return ie.excludes.Len() == 0 && (ie.includes.Len() == 0 || (ie.includes.Len() == 1 && ie.includes.Has("*")))
}

// ValidateIncludesExcludes checks provided lists of included and excluded
// items to ensure they are a valid set of IncludesExcludes data.
func ValidateIncludesExcludes(includesList, excludesList []string) []error {
	// TODO we should not allow an IncludesExcludes object to be created that
	// does not meet these criteria. Do a more significant refactoring to embed
	// this logic in object creation/modification.

	var errs []error

	includes := sets.NewString(includesList...)
	excludes := sets.NewString(excludesList...)

	if includes.Len() > 1 && includes.Has("*") {
		errs = append(errs, errors.New("includes list must either contain '*' only, or a non-empty list of items"))
	}

	if excludes.Has("*") {
		errs = append(errs, errors.New("excludes list cannot contain '*'"))
	}

	for _, itm := range excludes.List() {
		if includes.Has(itm) {
			errs = append(errs, errors.Errorf("excludes list cannot contain an item in the includes list: %v", itm))
		}
	}

	return errs
}

// ValidateNamespaceIncludesExcludes checks provided lists of included and
// excluded namespaces to ensure they are a valid set of IncludesExcludes data.
func ValidateNamespaceIncludesExcludes(includesList, excludesList []string) []error {
	errs := ValidateIncludesExcludes(includesList, excludesList)

	includes := sets.NewString(includesList...)
	excludes := sets.NewString(excludesList...)

	for _, itm := range includes.List() {
		// Although asterisks is not a valid Kubernetes namespace name, it is
		// allowed here.
		if itm != "*" {
			if nsErrs := validateNamespaceName(itm); nsErrs != nil {
				errs = append(errs, nsErrs...)
			}
		}
	}

	for _, itm := range excludes.List() {
		// Asterisks in excludes list have been checked previously.
		if itm != "*" {
			if nsErrs := validateNamespaceName(itm); nsErrs != nil {
				errs = append(errs, nsErrs...)
			}
		}
	}

	return errs
}

func validateNamespaceName(ns string) []error {
	var errs []error

	if errMsgs := validation.ValidateNamespaceName(ns, false); errMsgs != nil {
		for _, msg := range errMsgs {
			errs = append(errs, errors.Errorf("invalid namespace %q: %s", ns, msg))
		}
	}

	return errs
}

// GenerateIncludesExcludes constructs an IncludesExcludes struct by taking the provided
// include/exclude slices, applying the specified mapping function to each item in them,
// and adding the output of the function to the new struct. If the mapping function returns
// an empty string for an item, it is omitted from the result.
func GenerateIncludesExcludes(includes, excludes []string, mapFunc func(string) string) *IncludesExcludes {
	res := NewIncludesExcludes()

	for _, item := range includes {
		if item == "*" {
			res.Includes(item)
			continue
		}

		key := mapFunc(item)
		if key == "" {
			continue
		}
		res.Includes(key)
	}

	for _, item := range excludes {
		// wildcards are invalid for excludes,
		// so ignore them.
		if item == "*" {
			continue
		}

		key := mapFunc(item)
		if key == "" {
			continue
		}
		res.Excludes(key)
	}

	return res
}

// GetResourceIncludesExcludes takes the lists of resources to include and exclude, uses the
// discovery helper to resolve them to fully-qualified group-resource names, and returns an
// IncludesExcludes list.
func GetResourceIncludesExcludes(helper discovery.Helper, includes, excludes []string) *IncludesExcludes {
	resources := GenerateIncludesExcludes(
		includes,
		excludes,
		func(item string) string {
			gvr, _, err := helper.ResourceFor(schema.ParseGroupResource(item).WithVersion(""))
			if err != nil {
				// If we can't resolve it, return it as-is. This prevents the generated
				// includes-excludes list from including *everything*, if none of the includes
				// can be resolved. ref. https://github.com/vmware-tanzu/velero/issues/2461
				return item
			}

			gr := gvr.GroupResource()
			return gr.String()
		},
	)

	return resources
}
