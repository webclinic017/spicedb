package graph

import (
	core "github.com/authzed/spicedb/pkg/proto/core/v1"
	v1 "github.com/authzed/spicedb/pkg/proto/dispatch/v1"
)

// CheckResultsMap defines a type that is a map from resource ID to ResourceCheckResult.
// This must match that defined in the DispatchCheckResponse for the `results_by_resource_id`
// field.
type CheckResultsMap map[string]*v1.DispatchCheckResponse_ResourceCheckResult

// NewMembershipSet constructs a new helper set for tracking the membership found for a dispatched
// check request.
func NewMembershipSet() *MembershipSet {
	return &MembershipSet{
		hasDeterminedMember: false,
		membersByID:         map[string]*v1.CaveatExpression{},
	}
}

func membershipSetFromMap(mp map[string]*v1.CaveatExpression) *MembershipSet {
	ms := NewMembershipSet()
	for resourceID, result := range mp {
		ms.addMember(resourceID, result)
	}
	return ms
}

// MembershipSet is a helper set that trackes the membership results for a dispatched Check
// request, including tracking of the caveats associated with found resource IDs.
type MembershipSet struct {
	membersByID         map[string]*v1.CaveatExpression
	hasDeterminedMember bool
}

// AddDirectMember adds a resource ID that was *directly* found for the dispatched check, with
// optional caveat found on the relationship.
func (ms *MembershipSet) AddDirectMember(resourceID string, caveat *core.ContextualizedCaveat) {
	ms.addMember(resourceID, wrapCaveat(caveat))
}

// AddMemberViaRelationship adds a resource ID that was found via another relationship, such
// as the result of an arrow operation. The `parentRelationship` is the relationship that was
// followed before the resource itself was resolved. This method will properly apply the caveat(s)
// from both the parent relationship and the resource's result itself, assuming either have a caveat
// associated.
func (ms *MembershipSet) AddMemberViaRelationship(
	resourceID string,
	resourceCaveatExpression *v1.CaveatExpression,
	parentRelationship *core.RelationTuple,
) {
	intersection := caveatAnd(wrapCaveat(parentRelationship.Caveat), resourceCaveatExpression)
	ms.addMember(resourceID, intersection)
}

func (ms *MembershipSet) addMember(resourceID string, caveatExpr *v1.CaveatExpression) {
	existing, ok := ms.membersByID[resourceID]
	if !ok {
		ms.hasDeterminedMember = ms.hasDeterminedMember || caveatExpr == nil
		ms.membersByID[resourceID] = caveatExpr
		return
	}

	// If a determined membership result has already been found (i.e. there is no caveat),
	// then nothing more to do.
	if existing == nil {
		return
	}

	// If the new caveat expression is nil, then we are adding a determined result.
	if caveatExpr == nil {
		ms.hasDeterminedMember = true
		ms.membersByID[resourceID] = nil
		return
	}

	// Otherwise, the caveats get unioned together.
	ms.membersByID[resourceID] = caveatOr(existing, caveatExpr)
}

// UnionWith combines the results found in the given map with the members of this set.
// The changes are made in-place.
func (ms *MembershipSet) UnionWith(resultsMap CheckResultsMap) {
	for resourceID, details := range resultsMap {
		ms.addMember(resourceID, details.Expression)
	}
}

// IntersectWith intersects the results found in the given map with the members of this set.
// The changes are made in-place.
func (ms *MembershipSet) IntersectWith(resultsMap CheckResultsMap) {
	for resourceID := range ms.membersByID {
		if _, ok := resultsMap[resourceID]; !ok {
			delete(ms.membersByID, resourceID)
		}
	}

	ms.hasDeterminedMember = false
	for resourceID, details := range resultsMap {
		existing, ok := ms.membersByID[resourceID]
		if !ok {
			continue
		}
		if existing == nil && details.Expression == nil {
			ms.hasDeterminedMember = true
			continue
		}

		ms.membersByID[resourceID] = caveatAnd(existing, details.Expression)
	}
}

// Subtract subtracts the results found in the given map with the members of this set.
// The changes are made in-place.
func (ms *MembershipSet) Subtract(resultsMap CheckResultsMap) {
	ms.hasDeterminedMember = false
	for resourceID, expression := range ms.membersByID {
		if details, ok := resultsMap[resourceID]; ok {
			// If the incoming member has no caveat, then this removal is absolute.
			if details.Expression == nil {
				delete(ms.membersByID, resourceID)
				continue
			}

			// Otherwise, the caveat expression gets combined with an intersection of the inversion
			// of the expression.
			ms.membersByID[resourceID] = caveatSub(expression, details.Expression)
		} else {
			if expression == nil {
				ms.hasDeterminedMember = true
			}
		}
	}
}

// IsEmpty returns true if the set is empty.
func (ms *MembershipSet) IsEmpty() bool {
	return len(ms.membersByID) == 0
}

// HasDeterminedMember returns whether there exists at least one non-caveated member of the set.
func (ms *MembershipSet) HasDeterminedMember() bool {
	return ms.hasDeterminedMember
}

// AsCheckResultsMap converts the membership set back into a CheckResultsMap for placement into
// a DispatchCheckResult.
func (ms *MembershipSet) AsCheckResultsMap() CheckResultsMap {
	resultsMap := make(CheckResultsMap, len(ms.membersByID))
	for resourceID, caveat := range ms.membersByID {
		membership := v1.DispatchCheckResponse_MEMBER
		if caveat != nil {
			membership = v1.DispatchCheckResponse_CAVEATED_MEMBER
		}

		resultsMap[resourceID] = &v1.DispatchCheckResponse_ResourceCheckResult{
			Membership: membership,
			Expression: caveat,
		}
	}

	return resultsMap
}

func wrapCaveat(caveat *core.ContextualizedCaveat) *v1.CaveatExpression {
	if caveat == nil {
		return nil
	}

	return &v1.CaveatExpression{
		OperationOrCaveat: &v1.CaveatExpression_Caveat{
			Caveat: caveat,
		},
	}
}

func caveatOr(first *v1.CaveatExpression, second *v1.CaveatExpression) *v1.CaveatExpression {
	if first == nil {
		return second
	}

	if second == nil {
		return first
	}

	return &v1.CaveatExpression{
		OperationOrCaveat: &v1.CaveatExpression_Operation{
			Operation: &v1.CaveatOperation{
				Op:       v1.CaveatOperation_OR,
				Children: []*v1.CaveatExpression{first, second},
			},
		},
	}
}

func caveatAnd(first *v1.CaveatExpression, second *v1.CaveatExpression) *v1.CaveatExpression {
	if first == nil {
		return second
	}

	if second == nil {
		return first
	}

	return &v1.CaveatExpression{
		OperationOrCaveat: &v1.CaveatExpression_Operation{
			Operation: &v1.CaveatOperation{
				Op:       v1.CaveatOperation_AND,
				Children: []*v1.CaveatExpression{first, second},
			},
		},
	}
}

func invert(ce *v1.CaveatExpression) *v1.CaveatExpression {
	return &v1.CaveatExpression{
		OperationOrCaveat: &v1.CaveatExpression_Operation{
			Operation: &v1.CaveatOperation{
				Op:       v1.CaveatOperation_NOT,
				Children: []*v1.CaveatExpression{ce},
			},
		},
	}
}

func caveatSub(caveat *v1.CaveatExpression, subtraction *v1.CaveatExpression) *v1.CaveatExpression {
	inversion := invert(subtraction)
	if caveat == nil {
		return inversion
	}

	if subtraction == nil {
		panic("subtraction caveat expression is nil")
	}

	return &v1.CaveatExpression{
		OperationOrCaveat: &v1.CaveatExpression_Operation{
			Operation: &v1.CaveatOperation{
				Op:       v1.CaveatOperation_AND,
				Children: []*v1.CaveatExpression{caveat, inversion},
			},
		},
	}
}