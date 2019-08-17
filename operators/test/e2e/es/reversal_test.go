// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package es

import (
	"testing"

	common "github.com/elastic/cloud-on-k8s/operators/pkg/apis/common/v1alpha1"
	"github.com/elastic/cloud-on-k8s/operators/pkg/apis/elasticsearch/v1alpha1"
	"github.com/elastic/cloud-on-k8s/operators/test/e2e/test"
	"github.com/elastic/cloud-on-k8s/operators/test/e2e/test/elasticsearch"
)

func TestReversalIllegalConfig(t *testing.T) {
	// mutate to 1 m node + 1 d node
	b := elasticsearch.NewBuilder("test-illegal-config").
		WithNoESTopology().
		WithESDataNodes(1, elasticsearch.DefaultResources).
		WithESMasterNodes(1, elasticsearch.DefaultResources)

	bogus := b.WithAdditionalConfig(map[string]map[string]interface{}{
		"data": map[string]interface{}{
			"this leads": "to a bootlooping instance",
		},
	})

	state := elasticsearch.NewMutationReversalTestState(b.Elasticsearch)
	test.RunMutationReversal(t, []test.Builder{b}, []test.Builder{bogus}, state)
}

func TestReversalRiskyMasterDownscale(t *testing.T) {
	b := elasticsearch.NewBuilder("test-non-ha-downscale-reversal").
		WithESMasterDataNodes(2, elasticsearch.DefaultResources)
	down := b.WithNoESTopology().WithESMasterDataNodes(1, elasticsearch.DefaultResources)

	state := elasticsearch.NewMutationReversalTestState(b.Elasticsearch)
	test.RunMutationReversal(t, []test.Builder{b}, []test.Builder{down}, state)
}

// TODO we validate a full downscale
func TestReversalSingleMasterDownscale(t *testing.T) {
	t.Skip()
	b := elasticsearch.NewBuilder("test-non-ha-downscale-reversal").
		WithESMasterDataNodes(1, elasticsearch.DefaultResources)
	down := b.WithNoESTopology().WithESMasterDataNodes(0, elasticsearch.DefaultResources)

	state := elasticsearch.NewMutationReversalTestState(b.Elasticsearch)
	test.RunMutationReversal(t, []test.Builder{b}, []test.Builder{down}, state)
}

func TestReversalStatefulSetRename(t *testing.T) {
	b := elasticsearch.NewBuilder("test-sset-rename-reversal").
		WithESMasterDataNodes(1, elasticsearch.DefaultResources)

	copy := b.Elasticsearch.Spec.Nodes[0]
	copy.Name = "other"
	renamed := b.WithNoESTopology().WithNodeSpec(copy)

	state := elasticsearch.NewMutationReversalTestState(b.Elasticsearch)
	test.RunMutationReversal(t, []test.Builder{b}, []test.Builder{renamed}, state)
}

// TODO investigate why it does not apply the change here
func TestRiskyMasterReconfiguration(t *testing.T) {
	t.Skip()
	b := elasticsearch.NewBuilder("test-sset-reconfig-reversal").
		WithESMasterDataNodes(1, elasticsearch.DefaultResources).
		WithNodeSpec(v1alpha1.NodeSpec{
			Name:      "other-master",
			NodeCount: 1,
			Config: &common.Config{
				Data: map[string]interface{}{
					v1alpha1.NodeMaster: true,
					v1alpha1.NodeData:   true,
				},
			},
			PodTemplate: elasticsearch.ESPodTemplate(elasticsearch.DefaultResources),
		})

	// this currently breaks the cluster (something we might fix in the future at which point this just tests a temp downscale)
	noMasterMaster := b.WithNoESTopology().WithESMasterDataNodes(1, elasticsearch.DefaultResources).
		WithNodeSpec(v1alpha1.NodeSpec{
			Name:      "other-master",
			NodeCount: 1,
			Config: &common.Config{
				Data: map[string]interface{}{
					v1alpha1.NodeMaster: false,
					v1alpha1.NodeData:   true,
				},
			},
			PodTemplate: elasticsearch.ESPodTemplate(elasticsearch.DefaultResources),
		})

	state := elasticsearch.NewMutationReversalTestState(b.Elasticsearch)
	test.RunMutationReversal(t, []test.Builder{b}, []test.Builder{noMasterMaster}, state)
}