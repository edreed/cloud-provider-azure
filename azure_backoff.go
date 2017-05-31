/*
Copyright 2017 The Kubernetes Authors.

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

package azure

import (
	"net/http"
	"regexp"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/azure-sdk-for-go/arm/network"
	"github.com/Azure/go-autorest/autorest"
)

const (
	operationPollInterval        = 3 * time.Second
	operationPollTimeoutDuration = time.Hour
	backoffRetries               = 12
	backoffExponent              = 2
	backoffDuration              = 1 * time.Second
	backoffJitter                = 1.0
)

var azAPIBackoff = wait.Backoff{
	Steps:    backoffRetries,
	Factor:   backoffExponent,
	Duration: backoffDuration,
	Jitter:   backoffJitter,
}

// CreateOrUpdateSGWithRetry invokes az.SecurityGroupsClient.CreateOrUpdate with exponential backoff retry
func (az *Cloud) CreateOrUpdateSGWithRetry(sg network.SecurityGroup, delay time.Duration) error {
	return wait.ExponentialBackoff(azAPIBackoff, func() (bool, error) {
		az.operationPollRateLimiter.Accept()
		resp, err := az.SecurityGroupsClient.CreateOrUpdate(az.ResourceGroup, *sg.Name, sg, nil)
		return processRetryResponse(resp, err)
	})
}

// CreateOrUpdateLBWithRetry invokes az.LoadBalancerClient.CreateOrUpdate with exponential backoff retry
func (az *Cloud) CreateOrUpdateLBWithRetry(lb network.LoadBalancer) error {
	return wait.ExponentialBackoff(azAPIBackoff, func() (bool, error) {
		az.operationPollRateLimiter.Accept()
		resp, err := az.LoadBalancerClient.CreateOrUpdate(az.ResourceGroup, *lb.Name, lb, nil)
		return processRetryResponse(resp, err)
	})
}

// CreateOrUpdatePIPWithRetry invokes az.PublicIPAddressesClient.CreateOrUpdate with exponential backoff retry
func (az *Cloud) CreateOrUpdatePIPWithRetry(pip network.PublicIPAddress) error {
	return wait.ExponentialBackoff(azAPIBackoff, func() (bool, error) {
		az.operationPollRateLimiter.Accept()
		resp, err := az.PublicIPAddressesClient.CreateOrUpdate(az.ResourceGroup, *pip.Name, pip, nil)
		return processRetryResponse(resp, err)
	})
}

// CreateOrUpdateInterfaceWithRetry invokes az.PublicIPAddressesClient.CreateOrUpdate with exponential backoff retry
func (az *Cloud) CreateOrUpdateInterfaceWithRetry(nic network.Interface) error {
	return wait.ExponentialBackoff(azAPIBackoff, func() (bool, error) {
		az.operationPollRateLimiter.Accept()
		resp, err := az.InterfacesClient.CreateOrUpdate(az.ResourceGroup, *nic.Name, nic, nil)
		return processRetryResponse(resp, err)
	})
}

// DeletePublicIPWithRetry invokes az.PublicIPAddressesClient.Delete with exponential backoff retry
func (az *Cloud) DeletePublicIPWithRetry(pipName string) error {
	return wait.ExponentialBackoff(azAPIBackoff, func() (bool, error) {
		az.operationPollRateLimiter.Accept()
		resp, err := az.PublicIPAddressesClient.Delete(az.ResourceGroup, pipName, nil)
		return processRetryResponse(resp, err)
	})
}

// DeleteLBWithRetry invokes az.LoadBalancerClient.Delete with exponential backoff retry
func (az *Cloud) DeleteLBWithRetry(lbName string) error {
	return wait.ExponentialBackoff(azAPIBackoff, func() (bool, error) {
		az.operationPollRateLimiter.Accept()
		resp, err := az.LoadBalancerClient.Delete(az.ResourceGroup, lbName, nil)
		return processRetryResponse(resp, err)
	})
}

// CreateOrUpdateRouteTableWithRetry invokes az.RouteTablesClient.CreateOrUpdate with exponential backoff retry
func (az *Cloud) CreateOrUpdateRouteTableWithRetry(routeTable network.RouteTable) error {
	return wait.ExponentialBackoff(azAPIBackoff, func() (bool, error) {
		az.operationPollRateLimiter.Accept()
		resp, err := az.RouteTablesClient.CreateOrUpdate(az.ResourceGroup, az.RouteTableName, routeTable, nil)
		return processRetryResponse(resp, err)
	})
}

// CreateOrUpdateRouteWithRetry invokes az.RoutesClient.CreateOrUpdate with exponential backoff retry
func (az *Cloud) CreateOrUpdateRouteWithRetry(route network.Route) error {
	return wait.ExponentialBackoff(azAPIBackoff, func() (bool, error) {
		az.operationPollRateLimiter.Accept()
		resp, err := az.RoutesClient.CreateOrUpdate(az.ResourceGroup, az.RouteTableName, *route.Name, route, nil)
		return processRetryResponse(resp, err)
	})
}

// DeleteRouteWithRetry invokes az.RoutesClient.Delete with exponential backoff retry
func (az *Cloud) DeleteRouteWithRetry(routeName string) error {
	return wait.ExponentialBackoff(azAPIBackoff, func() (bool, error) {
		az.operationPollRateLimiter.Accept()
		resp, err := az.RoutesClient.Delete(az.ResourceGroup, az.RouteTableName, routeName, nil)
		return processRetryResponse(resp, err)
	})
}

// CreateOrUpdateVMWithRetry invokes az.VirtualMachinesClient.CreateOrUpdate with exponential backoff retry
func (az *Cloud) CreateOrUpdateVMWithRetry(vmName string, newVM compute.VirtualMachine) error {
	return wait.ExponentialBackoff(azAPIBackoff, func() (bool, error) {
		az.operationPollRateLimiter.Accept()
		resp, err := az.VirtualMachinesClient.CreateOrUpdate(az.ResourceGroup, vmName, newVM, nil)
		return processRetryResponse(resp, err)
	})
}

// An in-progress convenience function to deal with common HTTP backoff response conditions
func processRetryResponse(resp autorest.Response, err error) (bool, error) {
	if isSuccessHTTPResponse(resp) {
		return true, nil
	}
	if shouldRetryAPIRequest(resp, err) {
		return false, err
	}
	// TODO determine the complete set of short-circuit conditions
	if err != nil {
		return false, err
	}
	// Fall-through: stop periodic backoff, return error object from most recent request
	return true, err
}

// shouldRetryAPIRequest determines if the response from an HTTP request suggests periodic retry behavior
func shouldRetryAPIRequest(resp autorest.Response, err error) bool {
	if err != nil {
		return true
	}
	// HTTP 429 suggests we should retry
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	// HTTP 5xx suggests we should retry
	r, err := regexp.Compile(`^5\d\d$`)
	if err != nil {
		return false
	}
	if r.MatchString(strconv.Itoa(resp.StatusCode)) {
		return true
	}
	return false
}

// isSuccessHTTPResponse determines if the response from an HTTP request suggests success
func isSuccessHTTPResponse(resp autorest.Response) bool {
	// HTTP 2xx suggests a successful response
	r, err := regexp.Compile(`^2\d\d$`)
	if err != nil {
		return false
	}
	if r.MatchString(strconv.Itoa(resp.StatusCode)) {
		return true
	}
	return false
}
