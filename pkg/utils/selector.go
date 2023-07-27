/*
Copyright 2023 The KusionStack Authors.
Copyright 2021 The Kruise Authors.

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

package utils

import (
	"fmt"
	"reflect"
	"sort"
	"unsafe"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

func MergeLabelSelector(selA, selB *metav1.LabelSelector) *metav1.LabelSelector {
	if selB == nil {
		return selA
	}
	if selA == nil {
		return selB
	}
	result := selA.DeepCopy()
	if selB.MatchExpressions != nil {
		result.MatchExpressions = append(result.MatchExpressions, selB.MatchExpressions...)
	}
	if selB.MatchLabels != nil {
		if result.MatchLabels == nil {
			result.MatchLabels = map[string]string{}
		}
		for key, val := range selB.MatchLabels {
			result.MatchLabels[key] = val
		}
	}
	return result
}

func CombineLabelSelectors(ls ...labels.Selector) labels.Selector {
	var combined labels.Selector
	for _, l := range ls {
		if l == nil {
			continue
		}
		if combined == nil {
			combined = labels.NewSelector()
		}
		reqs, _ := l.Requirements()
		combined = combined.Add(reqs...)
	}
	return combined
}

func NegateLabelSelector(sel *metav1.LabelSelector) *metav1.LabelSelector {
	if sel == nil {
		return nil
	}
	newSel := &metav1.LabelSelector{}
	for k, v := range sel.MatchLabels {
		newSel.MatchExpressions = append(newSel.MatchExpressions, metav1.LabelSelectorRequirement{
			Key:      k,
			Operator: metav1.LabelSelectorOpNotIn,
			Values:   []string{v},
		})
	}
	sort.SliceStable(newSel.MatchExpressions, func(i, j int) bool { return newSel.MatchExpressions[i].Key < newSel.MatchExpressions[j].Key })

	for _, exp := range sel.MatchExpressions {
		var newOp metav1.LabelSelectorOperator
		switch exp.Operator {
		case metav1.LabelSelectorOpIn:
			newOp = metav1.LabelSelectorOpNotIn
		case metav1.LabelSelectorOpNotIn:
			newOp = metav1.LabelSelectorOpIn
		case metav1.LabelSelectorOpExists:
			newOp = metav1.LabelSelectorOpDoesNotExist
		case metav1.LabelSelectorOpDoesNotExist:
			newOp = metav1.LabelSelectorOpExists
		}
		newSel.MatchExpressions = append(newSel.MatchExpressions, metav1.LabelSelectorRequirement{
			Key:      exp.Key,
			Operator: newOp,
			Values:   exp.Values,
		})
	}
	return newSel
}

func ValidatedLabelSelectorAsSelector(ps *metav1.LabelSelector) (labels.Selector, error) {
	if ps == nil {
		return labels.Nothing(), nil
	}
	if len(ps.MatchLabels)+len(ps.MatchExpressions) == 0 {
		return labels.Everything(), nil
	}

	selector := labels.NewSelector()
	for k, v := range ps.MatchLabels {
		r, err := newRequirement(k, selection.Equals, []string{v})
		if err != nil {
			return nil, err
		}
		selector = selector.Add(*r)
	}
	for _, expr := range ps.MatchExpressions {
		var op selection.Operator
		switch expr.Operator {
		case metav1.LabelSelectorOpIn:
			op = selection.In
		case metav1.LabelSelectorOpNotIn:
			op = selection.NotIn
		case metav1.LabelSelectorOpExists:
			op = selection.Exists
		case metav1.LabelSelectorOpDoesNotExist:
			op = selection.DoesNotExist
		default:
			return nil, fmt.Errorf("%q is not a valid pod selector operator", expr.Operator)
		}
		r, err := newRequirement(expr.Key, op, append([]string(nil), expr.Values...))
		if err != nil {
			return nil, err
		}
		selector = selector.Add(*r)
	}
	return selector, nil
}

func newRequirement(key string, op selection.Operator, vals []string) (*labels.Requirement, error) {
	sel := &labels.Requirement{}
	selVal := reflect.ValueOf(sel)
	val := reflect.Indirect(selVal)

	keyField := val.FieldByName("key")
	keyFieldPtr := (*string)(unsafe.Pointer(keyField.UnsafeAddr()))
	*keyFieldPtr = key

	opField := val.FieldByName("operator")
	opFieldPtr := (*selection.Operator)(unsafe.Pointer(opField.UnsafeAddr()))
	*opFieldPtr = op

	if len(vals) > 0 {
		valuesField := val.FieldByName("strValues")
		valuesFieldPtr := (*[]string)(unsafe.Pointer(valuesField.UnsafeAddr()))
		*valuesFieldPtr = vals
	}

	return sel, nil
}
