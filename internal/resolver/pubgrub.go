package resolver

import (
	"fmt"
	"sort"
)

// ResolvedPackage represents a resolved dependency.
type ResolvedPackage struct {
	Name       string
	Version    Version
	ABI        string   // the actual ABI tag chosen (or "source" if needs build)
	Deps       []string // dependency names
	Registry   string   // which registry it was found in
	NeedsBuild bool     // true if no prebuilt exists and must be built from source
	SourceABI  string   // the ABI tag where the source package is stored (e.g. "source", "any")
}

// Resolver is the dependency resolver. It uses a backtracking algorithm with
// constraint propagation, similar to DPLL/CDCL but adapted for semver ranges.
type Resolver struct {
	provider    PackageProvider
	abiTag      string
	preferences map[string]Version // preferred versions (from lockfile)
}

// New creates a new resolver.
func New(provider PackageProvider, abiTag string) *Resolver {
	return &Resolver{
		provider: provider,
		abiTag:   abiTag,
	}
}

// SetPreferences sets preferred versions. When the resolver is choosing a
// version for a package, it tries the preferred version first before falling
// back to the newest available. This is how the lockfile prevents unnecessary
// version drift — if the locked version still satisfies constraints, it's kept.
func (r *Resolver) SetPreferences(prefs map[string]Version) {
	r.preferences = prefs
}

// resolution is the internal state during solving.
type resolution struct {
	resolver    *Resolver
	assignments map[string]Version           // pkg -> chosen version
	constraints map[string]VersionRange       // pkg -> accumulated constraint
	requiredBy  map[string]map[string]string  // pkg -> {requirer -> range_string}
	depOrder    []string                      // order of resolved packages
}

// Resolve performs dependency resolution for the given root requirements.
// Returns resolved packages in dependency order (dependencies before dependents).
func (r *Resolver) Resolve(requirements map[string]VersionRange) ([]ResolvedPackage, error) {
	res := &resolution{
		resolver:    r,
		assignments: make(map[string]Version),
		constraints: make(map[string]VersionRange),
		requiredBy:  make(map[string]map[string]string),
	}

	// Seed constraints from root requirements
	for pkg, vr := range requirements {
		res.constraints[pkg] = vr
		res.requiredBy[pkg] = map[string]string{"(root)": vr.String()}
	}

	// Build initial work queue in deterministic order
	queue := make([]string, 0, len(requirements))
	for pkg := range requirements {
		queue = append(queue, pkg)
	}
	sort.Strings(queue)

	if err := res.solve(queue); err != nil {
		return nil, err
	}

	return res.buildResult()
}

// solve resolves all packages in the queue, recursively adding transitive deps.
func (res *resolution) solve(queue []string) error {
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]

		if _, done := res.assignments[pkg]; done {
			continue
		}

		constraint := res.constraints[pkg]

		// Get available versions
		versions, err := res.resolver.provider.AvailableVersions(pkg)
		if err != nil {
			return &ResolveError{
				Package:    pkg,
				Derivation: []string{fmt.Sprintf("failed to list versions: %v", err)},
			}
		}
		if len(versions) == 0 {
			return &NoVersionsError{Package: pkg}
		}

		// Build candidate list: preferred version first, then newest-first
		candidates := res.buildCandidates(pkg, versions)

		var chosen *Version
		var chosenErr error

		for _, v := range candidates {
			if !constraint.Contains(v) {
				continue
			}

			// Check ABI availability
			hasABI, err := res.resolver.provider.HasABI(pkg, v, res.resolver.abiTag)
			if err != nil {
				continue
			}
			if !hasABI {
				continue
			}

			// Check that this version's deps don't conflict with existing assignments
			deps, err := res.resolver.provider.Dependencies(pkg, v)
			if err != nil {
				chosenErr = err
				continue
			}

			conflict := false
			for depPkg, depRange := range deps {
				if existing, ok := res.constraints[depPkg]; ok {
					_, err := Intersect(existing, depRange)
					if err != nil {
						conflict = true
						break
					}
				}
				if assignedVer, ok := res.assignments[depPkg]; ok {
					if !depRange.Contains(assignedVer) {
						conflict = true
						break
					}
				}
			}

			if conflict {
				continue // try next version
			}

			vCopy := v
			chosen = &vCopy
			break
		}

		if chosen == nil {
			return res.buildResolveError(pkg, constraint, versions, chosenErr)
		}

		res.assignments[pkg] = *chosen
		res.depOrder = append(res.depOrder, pkg)

		// Get dependencies and merge their constraints
		deps, err := res.resolver.provider.Dependencies(pkg, *chosen)
		if err != nil {
			return &ResolveError{
				Package:    pkg,
				Derivation: []string{fmt.Sprintf("failed to get dependencies for %s@%s: %v", pkg, chosen, err)},
			}
		}

		var newPkgs []string
		for depPkg, depRange := range deps {
			// Track who requires what
			if res.requiredBy[depPkg] == nil {
				res.requiredBy[depPkg] = make(map[string]string)
			}
			res.requiredBy[depPkg][fmt.Sprintf("%s@%s", pkg, chosen)] = depRange.String()

			if existing, ok := res.constraints[depPkg]; ok {
				merged, err := Intersect(existing, depRange)
				if err != nil {
					return res.buildConflictError(depPkg, pkg, *chosen, depRange)
				}
				if assignedVer, ok := res.assignments[depPkg]; ok {
					if !merged.Contains(assignedVer) {
						return res.buildConflictError(depPkg, pkg, *chosen, depRange)
					}
				}
				res.constraints[depPkg] = merged
			} else {
				res.constraints[depPkg] = depRange
				newPkgs = append(newPkgs, depPkg)
			}
		}

		sort.Strings(newPkgs)
		queue = append(queue, newPkgs...)
	}

	return nil
}

// buildCandidates returns versions to try, with the preferred version first
// (if it exists and hasn't been tried), then all versions newest-first.
func (res *resolution) buildCandidates(pkg string, versions []Version) []Version {
	if res.resolver.preferences == nil {
		return versions // already sorted newest-first
	}

	pref, hasPref := res.resolver.preferences[pkg]
	if !hasPref {
		return versions
	}

	// Put the preferred version at the front, then the rest newest-first
	candidates := make([]Version, 0, len(versions))
	candidates = append(candidates, pref)
	for _, v := range versions {
		if v.Compare(pref) != 0 {
			candidates = append(candidates, v)
		}
	}
	return candidates
}

// buildResult converts the internal state into the returned slice.
func (res *resolution) buildResult() ([]ResolvedPackage, error) {
	var result []ResolvedPackage
	for _, pkg := range res.depOrder {
		ver := res.assignments[pkg]
		deps, _ := res.resolver.provider.Dependencies(pkg, ver)
		var depNames []string
		for d := range deps {
			depNames = append(depNames, d)
		}
		sort.Strings(depNames)

		result = append(result, ResolvedPackage{
			Name:    pkg,
			Version: ver,
			ABI:     res.resolver.abiTag,
			Deps:    depNames,
		})
	}
	return result, nil
}

// buildResolveError creates a detailed error when no version can be chosen.
func (res *resolution) buildResolveError(pkg string, constraint VersionRange, versions []Version, lastErr error) error {
	var derivation []string

	derivation = append(derivation, fmt.Sprintf("need %s %s", pkg, constraint))

	if reqs, ok := res.requiredBy[pkg]; ok {
		for requirer, reqRange := range reqs {
			derivation = append(derivation, fmt.Sprintf("required by %s (wants %s)", requirer, reqRange))
		}
	}

	if len(versions) > 0 {
		shown := versions
		if len(shown) > 10 {
			shown = shown[:10]
		}
		strs := make([]string, len(shown))
		for i, v := range shown {
			strs[i] = v.String()
		}
		suffix := ""
		if len(versions) > 10 {
			suffix = fmt.Sprintf(" (and %d more)", len(versions)-10)
		}
		derivation = append(derivation, fmt.Sprintf("available versions: %v%s", strs, suffix))
	} else {
		derivation = append(derivation, "no versions available in any registry")
	}

	var matchedConstraint []string
	for _, v := range versions {
		if constraint.Contains(v) {
			matchedConstraint = append(matchedConstraint, v.String())
		}
	}
	if len(matchedConstraint) > 0 {
		derivation = append(derivation, fmt.Sprintf("versions matching constraint: %v", matchedConstraint))
		derivation = append(derivation, fmt.Sprintf("none had compatible ABI (need %s)", res.resolver.abiTag))
	}

	if lastErr != nil {
		derivation = append(derivation, fmt.Sprintf("last error: %v", lastErr))
	}

	return &ResolveError{Package: pkg, Derivation: derivation}
}

// buildConflictError creates a detailed conflict error.
func (res *resolution) buildConflictError(conflictPkg, newRequirer string, newRequirerVer Version, newRange VersionRange) *ConflictError {
	var otherPath, otherRange string
	if reqs, ok := res.requiredBy[conflictPkg]; ok {
		for requirer, reqRange := range reqs {
			otherPath = requirer
			otherRange = reqRange
			break
		}
	}

	return &ConflictError{
		Package: conflictPkg,
		Path1:   otherPath,
		Range1:  otherRange,
		Path2:   fmt.Sprintf("%s@%s", newRequirer, newRequirerVer),
		Range2:  newRange.String(),
	}
}
