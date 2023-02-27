/*
Copyright 2023 The Kubernetes Authors.

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
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"

	custompolicy "sigs.k8s.io/cloud-provider-azure/pkg/azureclients/v2/policy"
)

func GetDefaultOption(apiVersion string) *arm.ClientOptions {
	return &arm.ClientOptions{
		ClientOptions: policy.ClientOptions{
			Retry: policy.RetryOptions{
				RetryDelay:    custompolicy.DefaultRetryDelay,
				MaxRetryDelay: custompolicy.DefaultMaxRetryDelay,
				MaxRetries:    custompolicy.DefaultMaxRetries,
				TryTimeout:    custompolicy.DefaultTryTimeout,
				StatusCodes:   custompolicy.GetRetriableStatusCode(),
			},
			PerRetryPolicies: []policy.Policy{
				custompolicy.NewThrottlingPolicy(),
			},
			APIVersion: apiVersion,
			Transport:  defaultHTTPClient,
		},
	}
}
