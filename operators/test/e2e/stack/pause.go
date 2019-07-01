// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

/*
 * Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
 * or more contributor license agreements. Licensed under the Elastic License;
 * you may not use this file except in compliance with the Elastic License.
 */

package stack

import (
	"fmt"
	"strconv"

	"github.com/elastic/cloud-on-k8s/operators/pkg/apis/elasticsearch/v1alpha1"
	"github.com/elastic/cloud-on-k8s/operators/pkg/controller/common"
	"github.com/elastic/cloud-on-k8s/operators/pkg/utils/k8s"
	"github.com/elastic/cloud-on-k8s/operators/test/e2e/helpers"
)

func togglePauseOn(paused bool, es v1alpha1.Elasticsearch, k *helpers.K8sHelper) helpers.TestStep {
	return helpers.TestStep{
		Name: fmt.Sprintf("Should pause reconciliation %v", paused),
		Test: helpers.Eventually(func() error {
			var curr v1alpha1.Elasticsearch
			if err := k.Client.Get(k8s.ExtractNamespacedName(&es), &curr); err != nil {
				return err
			}
			as := curr.Annotations
			if as == nil {
				as = map[string]string{}
			}
			as[common.PauseAnnotationName] = strconv.FormatBool(paused)
			curr.Annotations = as
			return k.Client.Update(&curr)
		}),
	}
}

func PauseReconciliation(es v1alpha1.Elasticsearch, k *helpers.K8sHelper) helpers.TestStep {
	return togglePauseOn(true, es, k)
}

func ResumeReconciliation(es v1alpha1.Elasticsearch, k *helpers.K8sHelper) helpers.TestStep {
	return togglePauseOn(false, es, k)
}
