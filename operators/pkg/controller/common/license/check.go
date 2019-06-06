// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package license

import (
	"time"

	"github.com/elastic/cloud-on-k8s/operators/pkg/apis/elasticsearch/v1alpha1"
	"github.com/elastic/cloud-on-k8s/operators/pkg/utils/k8s"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type Checker interface {
	EnterpriseFeaturesEnabled() (bool, error)
	Valid(l SourceEnterpriseLicense) (bool, error)
}

// Checker contains parameters for license checks.
type checker struct {
	k8sClient         k8s.Client
	operatorNamespace string
	publicKey         []byte
}

// NewLicenseChecker creates a new license checker.
func NewLicenseChecker(client k8s.Client, operatorNamespace string) Checker {
	return &checker{
		k8sClient:         client,
		operatorNamespace: operatorNamespace,
		publicKey:         publicKeyBytes,
	}
}

func (lc *checker) publicKeyFor(l SourceEnterpriseLicense) ([]byte, error) {
	if !l.IsTrial() {
		return lc.publicKey, nil
	}
	var signatureSec corev1.Secret
	return signatureSec.Data[TrialPubkeyKey], lc.k8sClient.Get(types.NamespacedName{
		Namespace: lc.operatorNamespace,
		Name:      TrialStatusSecretKey,
	}, &signatureSec)
}

// EnterpriseFeaturesEnabled returns true if a valid enterprise license is installed.
func (lc *checker) EnterpriseFeaturesEnabled() (bool, error) {
	licenses, err := EnterpriseLicenses(lc.k8sClient)
	if err != nil {
		return false, errors.Wrap(err, "failed to list enterprise licenses")
	}

	for _, l := range licenses {
		valid, err := lc.Valid(l)
		if err != nil {
			return false, err
		}
		if valid {
			return true, nil
		}
	}
	return false, nil
}

func (lc *checker) Valid(l SourceEnterpriseLicense) (bool, error) {
	pk, err := lc.publicKeyFor(l)
	if err != nil {
		return false, errors.Wrap(err, "while loading signature secret")
	}
	verifier, err := NewVerifier(pk)
	if err != nil {
		log.Error(err, "while creating license verifier")
		return false, nil
	}
	status := verifier.Valid(l, time.Now())
	if status == v1alpha1.LicenseStatusValid {
		return true, nil
	}
	return false, nil
}

type MockChecker struct{}

func (MockChecker) EnterpriseFeaturesEnabled() (bool, error) {
	return true, nil
}

func (MockChecker) Valid(l SourceEnterpriseLicense) (bool, error) {
	return true, nil
}

var _ Checker = MockChecker{}
