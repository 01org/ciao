/*
// Copyright (c) 2016 Intel Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
*/
// @SubApi Servers API [/v2.1/{tenant}/servers]
// @SubApi Flavors API [/v2.1/{tenant}/flavors]
// @SubApi Resources API [/v2.1/{tenant}/resources]
// @SubApi Quotas API [/v2.1/{tenant}/quotas]
// @SubApi Events API [/v2.1/{tenant}/events]
// @SubApi Nodes API [/v2.1/nodes]
// @SubApi Tenants API [/v2.1/tenants]
// @SubApi CNCIs API [/v2.1/cncis]
// @SubApi Traces API [/v2.1/traces]

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/01org/ciao/ciao-controller/types"
	"github.com/01org/ciao/payloads"
	"github.com/01org/ciao/ssntp"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
)

const openstackComputeAPIPort = 8774

type action uint8

const (
	computeActionStart action = iota
	computeActionStop
	computeActionDelete
)

type pagerFilterType uint8

const (
	none           pagerFilterType = 0
	workloadFilter                 = 0x1
	statusFilter                   = 0x2
)

type pager interface {
	filter(filterType pagerFilterType, filter string, item interface{}) bool
	nextPage(filterType pagerFilterType, filter string, r *http.Request) ([]byte, error)
}

type serverPager struct {
	context   *controller
	instances []*types.Instance
}

func dumpRequestBody(r *http.Request, body bool) {
	if glog.V(2) {
		dump, err := httputil.DumpRequest(r, body)
		if err != nil {
			glog.Errorf("HTTP request dump error %s", err)
		}

		glog.Infof("HTTP request [%q]", dump)
	}
}

func dumpRequest(r *http.Request) {
	dumpRequestBody(r, false)
}

func pagerQueryParse(r *http.Request) (int, int, string) {
	values := r.URL.Query()
	limit := 0
	offset := 0
	marker := ""
	if values["limit"] != nil {
		l, err := strconv.ParseInt(values["limit"][0], 10, 32)
		if err != nil {
			limit = 0
		} else {
			limit = (int)(l)
		}
	}

	if values["marker"] != nil {
		marker = values["marker"][0]
	} else if values["offset"] != nil {
		o, err := strconv.ParseInt(values["offset"][0], 10, 32)
		if err != nil {
			offset = 0
		} else {
			offset = (int)(o)
		}
	}

	return limit, offset, marker
}

func (pager *serverPager) getInstances(filterType pagerFilterType, filter string, instances []*types.Instance, limit int, offset int) ([]byte, error) {
	servers := payloads.NewComputeServers()

	servers.TotalServers = len(instances)
	pageLength := 0

	glog.V(2).Infof("Get instances limit [%d] offset [%d]", limit, offset)

	if instances == nil || offset >= len(instances) {
		b, err := json.Marshal(servers)
		if err != nil {
			return nil, err
		}

		return b, nil
	}

	for _, instance := range instances[offset:] {
		if filterType != none && pager.filter(filterType, filter, instance) {
			continue
		}

		server, err := instanceToServer(pager.context, instance)
		if err != nil {
			return nil, err
		}

		servers.Servers = append(servers.Servers, server)
		pageLength++
		if limit > 0 && pageLength >= limit {
			break
		}

	}

	b, err := json.Marshal(servers)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (pager *serverPager) filter(filterType pagerFilterType, filter string, instance *types.Instance) bool {
	switch filterType {
	case workloadFilter:
		if instance.WorkloadID != filter {
			return true
		}
	}

	return false
}

func (pager *serverPager) nextPage(filterType pagerFilterType, filter string, r *http.Request) ([]byte, error) {
	limit, offset, lastSeen := pagerQueryParse(r)

	glog.V(2).Infof("Next page marker [%s] limit [%d] offset [%d]", lastSeen, limit, offset)

	if lastSeen == "" {
		if limit != 0 {
			return pager.getInstances(filterType, filter, pager.instances, limit, offset)
		}

		return pager.getInstances(filterType, filter, pager.instances, 0, offset)
	}

	for i, instance := range pager.instances {
		if instance.ID == lastSeen {
			if i >= len(pager.instances)-1 {
				return pager.getInstances(filterType, filter, nil, limit, 0)
			}

			return pager.getInstances(filterType, filter, pager.instances[i+1:], limit, 0)
		}
	}

	return nil, fmt.Errorf("Item %s not found", lastSeen)
}

type nodePager struct {
	context *controller
	nodes   []payloads.CiaoComputeNode
}

func (pager *nodePager) getNodes(filterType pagerFilterType, filter string, nodes []payloads.CiaoComputeNode, limit int, offset int) ([]byte, error) {
	computeNodes := payloads.NewCiaoComputeNodes()

	pageLength := 0

	glog.V(2).Infof("Get nodes limit [%d] offset [%d]", limit, offset)

	if nodes == nil || offset >= len(nodes) {
		b, err := json.Marshal(computeNodes)
		if err != nil {
			return nil, err
		}

		return b, nil
	}

	for _, node := range nodes[offset:] {
		computeNodes.Nodes = append(computeNodes.Nodes, node)

		pageLength++
		if limit > 0 && pageLength >= limit {
			break
		}
	}

	b, err := json.Marshal(computeNodes)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (pager *nodePager) filter(filterType pagerFilterType, filter string, node payloads.CiaoComputeNode) bool {
	return false
}

func (pager *nodePager) nextPage(filterType pagerFilterType, filter string, r *http.Request) ([]byte, error) {
	limit, offset, lastSeen := pagerQueryParse(r)

	glog.V(2).Infof("Next page marker [%s] limit [%d] offset [%d]", lastSeen, limit, offset)

	if lastSeen == "" {
		if limit != 0 {
			return pager.getNodes(filterType, filter, pager.nodes, limit, offset)
		}

		return pager.getNodes(filterType, filter, pager.nodes, 0, offset)
	}

	for i, node := range pager.nodes {
		if node.ID == lastSeen {
			if i >= len(pager.nodes)-1 {
				return pager.getNodes(filterType, filter, nil, limit, 0)
			}

			return pager.getNodes(filterType, filter, pager.nodes[i+1:], limit, 0)
		}
	}

	return nil, fmt.Errorf("Item %s not found", lastSeen)
}

type nodeServerPager struct {
	context   *controller
	instances []payloads.CiaoServerStats
}

func (pager *nodeServerPager) getNodeServers(filterType pagerFilterType, filter string, instances []payloads.CiaoServerStats,
	limit int, offset int) ([]byte, error) {
	servers := payloads.NewCiaoServersStats()

	servers.TotalServers = len(instances)
	pageLength := 0

	glog.V(2).Infof("Get nodes limit [%d] offset [%d]", limit, offset)

	if instances == nil || offset >= len(instances) {
		b, err := json.Marshal(servers)
		if err != nil {
			return nil, err
		}

		return b, nil
	}

	for _, instance := range instances[offset:] {
		servers.Servers = append(servers.Servers, instance)

		pageLength++
		if limit > 0 && pageLength >= limit {
			break
		}
	}

	b, err := json.Marshal(servers)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (pager *nodeServerPager) filter(filterType pagerFilterType, filter string, instance payloads.CiaoServerStats) bool {
	return false
}

func (pager *nodeServerPager) nextPage(filterType pagerFilterType, filter string, r *http.Request) ([]byte, error) {
	limit, offset, lastSeen := pagerQueryParse(r)

	glog.V(2).Infof("Next page marker [%s] limit [%d] offset [%d]", lastSeen, limit, offset)

	if lastSeen == "" {
		if limit != 0 {
			return pager.getNodeServers(filterType, filter, pager.instances, limit, offset)
		}

		return pager.getNodeServers(filterType, filter, pager.instances, 0, offset)
	}

	for i, instance := range pager.instances {
		if instance.ID == lastSeen {
			if i >= len(pager.instances)-1 {
				return pager.getNodeServers(filterType, filter, nil, limit, 0)
			}

			return pager.getNodeServers(filterType, filter, pager.instances[i+1:], limit, 0)
		}
	}

	return nil, fmt.Errorf("Item %s not found", lastSeen)
}

func tenantToken(context *controller, r *http.Request, tenant string) bool {
	var validServices = []struct {
		serviceType string
		serviceName string
	}{
		{
			serviceType: "compute",
			serviceName: "ciao",
		},
		{
			serviceType: "compute",
			serviceName: "nova",
		},
	}
	token := r.Header["X-Auth-Token"]
	if token == nil {
		return false
	}

	/* TODO Caching or PKI */
	for _, s := range validServices {
		if context.id.validateService(token[0], tenant, s.serviceType, s.serviceName) == true {
			return true
		}

	}

	for _, s := range validServices {
		if context.id.validateService(token[0], tenant, s.serviceType, "") == true {
			return true
		}

	}

	return false
}

func adminToken(context *controller, r *http.Request) bool {
	var validAdmins = []struct {
		project string
		role    string
	}{
		{
			project: "service",
			role:    "admin",
		},
		{
			project: "admin",
			role:    "admin",
		},
	}

	token := r.Header["X-Auth-Token"]
	if token == nil {
		return false
	}

	/* TODO Caching or PKI */
	for _, a := range validAdmins {
		if context.id.validateProjectRole(token[0], a.project, a.role) == true {
			return true
		}
	}

	vars := mux.Vars(r)
	tenant := vars["tenant"]
	glog.V(2).Infof("Invalid token for [%s]", tenant)
	return false
}

func validateToken(context *controller, r *http.Request) bool {
	vars := mux.Vars(r)
	tenant := vars["tenant"]

	glog.V(2).Infof("Token validation for [%s]", tenant)

	// We do not want to unconditionally check for an admin token, this is inefficient.
	// We check for an admin token iff:
	// - We do not have a tenant variable
	// - We do have one but it does not match the token

	/* If we don't have a tenant parameter, are we admin ? */
	if tenant == "" {
		return adminToken(context, r)
	}

	/* If we have a tenant parameter that does not match the token are we admin ? */
	if tenantToken(context, r, tenant) == false {
		return adminToken(context, r)
	}

	return true
}

func instanceToServer(context *controller, instance *types.Instance) (payloads.Server, error) {
	workload, err := context.ds.GetWorkload(instance.WorkloadID)
	if err != nil {
		return payloads.Server{}, err
	}

	imageID := workload.ImageID

	server := payloads.Server{
		HostID:   instance.NodeID,
		ID:       instance.ID,
		TenantID: instance.TenantID,
		Flavor: payloads.Flavor{
			ID: instance.WorkloadID,
		},
		Image: payloads.Image{
			ID: imageID,
		},
		Status: instance.State,
		Addresses: payloads.Addresses{
			Private: []payloads.PrivateAddresses{
				{
					Addr:               instance.IPAddress,
					OSEXTIPSMACMacAddr: instance.MACAddress,
				},
			},
		},
		SSHIP:   instance.SSHIP,
		SSHPort: instance.SSHPort,
	}

	return server, nil
}

// returnErrorCode returns error codes for the http call
func returnErrorCode(w http.ResponseWriter, httpError int, messageFormat string, messageArgs ...interface{}) {
	var returnCode payloads.HTTPReturnErrorCode
	returnCode.Error.Code = httpError
	returnCode.Error.Name = http.StatusText(returnCode.Error.Code)

	returnCode.Error.Message = fmt.Sprintf(messageFormat, messageArgs...)

	b, err := json.Marshal(returnCode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Error(w, string(b), httpError)
}

// @Title showServerDetails
// @Description Shows details for a server.
// @Accept  json
// @Success 200 {object} payloads.ComputeServer "Returns details for a server."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/{tenant}/servers/{server} [get]
// @Resource /v2.1/{tenant}/servers
func showServerDetails(w http.ResponseWriter, r *http.Request, context *controller) {
	vars := mux.Vars(r)
	tenant := vars["tenant"]
	instanceID := vars["server"]
	var server payloads.ComputeServer

	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	instance, err := context.ds.GetInstance(instanceID)
	if err != nil {
		returnErrorCode(w, http.StatusNotFound, "Instance could not be found")
		return
	}

	if instance.TenantID != tenant {
		returnErrorCode(w, http.StatusNotFound, "Instance does not belong to tenant")
		return
	}

	server.Server, err = instanceToServer(context, instance)
	if err != nil {
		returnErrorCode(w, http.StatusNotFound, "Instance could not be found")
		return
	}

	b, err := json.Marshal(server)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title deleteServer
// @Description Deletes a server.
// @Accept  json
// @Success 202 {object} string "This operation does not return a response body, returns the 202 StatusAccepted code."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/{tenant}/servers/{server} [delete]
// @Resource /v2.1/{tenant}/servers
func deleteServer(w http.ResponseWriter, r *http.Request, context *controller) {
	vars := mux.Vars(r)
	tenant := vars["tenant"]
	instance := vars["server"]

	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	/* First check that the instance belongs to this tenant */
	i, err := context.ds.GetInstance(instance)
	if err != nil {
		returnErrorCode(w, http.StatusNotFound, "Instance could not be found")
		return
	}

	if i.TenantID != tenant {
		returnErrorCode(w, http.StatusNotFound, "Instance does not belong to tenant")
		return
	}

	err = context.deleteInstance(instance)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func buildFlavorDetails(workload *types.Workload) (payloads.FlavorDetails, error) {
	var details payloads.FlavorDetails

	defaults := workload.Defaults
	if len(defaults) == 0 {
		return details, fmt.Errorf("Workload resources not set")
	}

	details.OsFlavorAccessIsPublic = true
	details.ID = workload.ID
	details.Disk = workload.ImageID
	details.Name = workload.Description

	for r := range defaults {
		switch defaults[r].Type {
		case payloads.VCPUs:
			details.Vcpus = defaults[r].Value
		case payloads.MemMB:
			details.RAM = defaults[r].Value
		}
	}

	return details, nil
}

// @Title listFlavors
// @Description Lists flavors.
// @Accept  json
// @Success 200 {array} interface "Returns payloads.NewComputeFlavors() with the corresponding available flavors for the tenant."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/{tenant}/flavors [get]
// @Resource /v2.1/{tenant}/flavors
func listFlavors(w http.ResponseWriter, r *http.Request, context *controller) {
	flavors := payloads.NewComputeFlavors()

	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	workloads, err := context.ds.GetWorkloads()
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, workload := range workloads {
		flavors.Flavors = append(flavors.Flavors,
			struct {
				ID    string          `json:"id"`
				Links []payloads.Link `json:"links"`
				Name  string          `json:"name"`
			}{
				ID:   workload.ID,
				Name: workload.Description,
			},
		)
	}

	b, err := json.Marshal(flavors)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title listFlavorsDetails
// @Description Lists flavors with details.
// @Accept  json
// @Success 200 {array} interface "Returns payloads.NewComputeFlavorsDetails() of flavor details."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/{tenant}/flavors/detail [get]
// @Resource /v2.1/{tenant}/flavors
func listFlavorsDetails(w http.ResponseWriter, r *http.Request, context *controller) {
	var details payloads.FlavorDetails
	flavors := payloads.NewComputeFlavorsDetails()

	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	workloads, err := context.ds.GetWorkloads()
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, workload := range workloads {
		details, err = buildFlavorDetails(workload)
		if err != nil {
			continue
		}

		flavors.Flavors = append(flavors.Flavors, details)
	}

	b, err := json.Marshal(flavors)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title showFlavorDetails
// @Description Shows details for a flavor.
// @Accept  json
// @Success 200 {object} payloads.ComputeFlavorDetails "Returns details for a flavor."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/{tenant}/flavors/{flavor} [get]
// @Resource /v2.1/{tenant}/flavors
func showFlavorDetails(w http.ResponseWriter, r *http.Request, context *controller) {
	vars := mux.Vars(r)
	workloadID := vars["flavor"]
	var flavor payloads.ComputeFlavorDetails

	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	workload, err := context.ds.GetWorkload(workloadID)
	if err != nil {
		returnErrorCode(w, http.StatusNotFound, "Workload not found")
		return
	}

	details, err := buildFlavorDetails(workload)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	flavor.Flavor = details

	b, err := json.Marshal(flavor)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title listFlavorServerDetail
// @Description Lists all servers with details.
// @Accept  json
// @Success 200 {array} types.Instance "Returns a list of all servers."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/flavors/{flavor}/servers/detail [get]
// @Resource /v2.1/{tenant}/flavors
// listFlavorServerDetail is created with the only purpose of API documentation for method
// /v2.1/flavors/{flavor}/servers/detail [get]
func listFlavorServerDetail(w http.ResponseWriter, r *http.Request, context *controller) {
	listServerDetails(w, r, context)
}

const (
	instances int = 1
	vcpu          = 2
	memory        = 3
	disk          = 4
)

// @Title listTenantQuotas
// @Description List the use of all resources used of a tenant from a start to end point of time.
// @Accept  json
// @Success 200 {object} payloads.CiaoTenantResources "Returns the limits and usage of resources of a tenant."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/{tenant}/quotas [get]
// @Resource /v2.1/{tenant}/quotas
func listTenantQuotas(w http.ResponseWriter, r *http.Request, context *controller) {
	vars := mux.Vars(r)
	tenant := vars["tenant"]
	var tenantResource payloads.CiaoTenantResources

	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	t, err := context.ds.GetTenant(tenant)
	if err != nil {
		returnErrorCode(w, http.StatusNotFound, "Tenant could not be found")
		return
	}

	if t == nil {
		if *noNetwork {
			_, err := context.ds.AddTenant(tenant)
			if err != nil {
				returnErrorCode(w, http.StatusInternalServerError, err.Error())
				return
			}
		} else {
			err = context.addTenant(tenant)
			if err != nil {
				returnErrorCode(w, http.StatusInternalServerError, err.Error())
				return
			}
		}

		t, err = context.ds.GetTenant(tenant)
		if err != nil {
			returnErrorCode(w, http.StatusNotFound, "Tenant could not be found")
			return
		}
	}

	resources := t.Resources

	tenantResource.ID = t.ID

	for _, resource := range resources {
		switch resource.Rtype {
		case instances:
			tenantResource.InstanceLimit = resource.Limit
			tenantResource.InstanceUsage = resource.Usage

		case vcpu:
			tenantResource.VCPULimit = resource.Limit
			tenantResource.VCPUUsage = resource.Usage

		case memory:
			tenantResource.MemLimit = resource.Limit
			tenantResource.MemUsage = resource.Usage

		case disk:
			tenantResource.DiskLimit = resource.Limit
			tenantResource.DiskUsage = resource.Usage
		}
	}

	b, err := json.Marshal(tenantResource)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func tenantQueryParse(r *http.Request) (time.Time, time.Time, error) {
	values := r.URL.Query()
	var startTime, endTime time.Time

	if values["start_date"] == nil || values["end_date"] == nil {
		return startTime, endTime, fmt.Errorf("Missing date")
	}

	startTime, err := time.Parse(time.RFC3339, values["start_date"][0])
	if err != nil {
		return startTime, endTime, err
	}

	endTime, err = time.Parse(time.RFC3339, values["end_date"][0])
	if err != nil {
		return startTime, endTime, err
	}

	return startTime, endTime, nil
}

// @Title listTenantResources
// @Description List the use of all resources used of a tenant from a start to end point of time.
// @Accept  json
// @Success 200 {object} payloads.CiaoUsageHistory "Returns the usage of resouces."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/{tenant}/resources [get]
// @Resource /v2.1/{tenant}/resources
func listTenantResources(w http.ResponseWriter, r *http.Request, context *controller) {
	vars := mux.Vars(r)
	tenant := vars["tenant"]
	var usage payloads.CiaoUsageHistory

	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	start, end, err := tenantQueryParse(r)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	glog.V(2).Infof("Start %v\n", start)
	glog.V(2).Infof("End %v\n", end)

	usage.Usages, err = context.ds.GetTenantUsage(tenant, start, end)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	b, err := json.Marshal(usage)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title listServerDetails
// @Description Lists all servers with details.
// @Accept  json
// @Success 200 {array} types.Instance "Returns details of all servers."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/{tenant}/servers/detail [get]
// @Resource /v2.1/{tenant}/servers
func listServerDetails(w http.ResponseWriter, r *http.Request, context *controller) {
	vars := mux.Vars(r)
	tenant := vars["tenant"]
	workload := vars["flavor"]
	var instances []*types.Instance
	var err error

	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	if tenant != "" {
		instances, err = context.ds.GetAllInstancesFromTenant(tenant)
	} else {
		instances, err = context.ds.GetAllInstances()
	}

	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	sort.Sort(types.SortedInstancesByID(instances))

	pager := serverPager{
		context:   context,
		instances: instances,
	}

	filterType := none
	filter := ""
	if workload != "" {
		filterType = workloadFilter
		filter = workload
	}

	b, err := pager.nextPage(filterType, filter, r)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title createServer
// @Description Creates a server.
// @Accept  json
// @Success 202 {object} payloads.Server "Returns payloads.ComputeCreateServer and payloads.ComputeServer with data of the created server."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/{tenant}/servers [post]
// @Resource /v2.1/{tenant}/servers
func createServer(w http.ResponseWriter, r *http.Request, context *controller) {
	vars := mux.Vars(r)
	tenant := vars["tenant"]
	var server payloads.ComputeCreateServer
	var servers payloads.ComputeServers

	dumpRequestBody(r, true)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	defer r.Body.Close()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		returnErrorCode(w, http.StatusBadRequest, "Service cannot read Request Body")
		return
	}

	err = json.Unmarshal(body, &server)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	nInstances := 1

	if server.Server.MaxInstances > 0 {
		nInstances = server.Server.MaxInstances
	} else if server.Server.MinInstances > 0 {
		nInstances = server.Server.MinInstances
	}

	trace := false
	label := ""
	if server.Server.Name != "" {
		trace = true
		label = server.Server.Name
	}
	instances, err := context.startWorkload(server.Server.Workload, tenant, nInstances, trace, label)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, instance := range instances {
		server, err := instanceToServer(context, instance)
		if err != nil {
			returnErrorCode(w, http.StatusInternalServerError, err.Error())
			return
		}
		servers.Servers = append(servers.Servers, server)
	}
	servers.TotalServers = len(instances)

	// set machine ID for OpenStack compatibility
	server.Server.ID = instances[0].ID

	// builtServers is define to meet OpenStack compatibility on result format and keep CIAOs
	builtServers := struct {
		payloads.ComputeCreateServer
		payloads.ComputeServers
	}{
		payloads.ComputeCreateServer{
			Server: server.Server,
		},
		payloads.ComputeServers{
			TotalServers: servers.TotalServers,
			Servers:      servers.Servers,
		},
	}

	b, err := json.Marshal(builtServers)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write(b)
}

type instanceAction func(string) error

// @Title tenantServersAction
// @Description Runs the indicated action (os-start, os-stop, os-delete) in the servers.
// @Accept  json
// @Success 202 {object} string "This operation does not return a response body, returns the 202 StatusAccepted code."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/{tenant}/servers/action [post]
// @Resource /v2.1/{tenant}/servers
// tenantServersAction will apply the operation sent in POST (as os-start, os-stop, os-delete)
// to all servers of a tenant or if ServersID size is greater than zero it will be applied
// only to the subset provided that also belongs to the tenant
func tenantServersAction(w http.ResponseWriter, r *http.Request, context *controller) {
	vars := mux.Vars(r)
	tenant := vars["tenant"]
	var servers payloads.CiaoServersAction
	var actionFunc instanceAction
	var statusFilter string

	dumpRequestBody(r, true)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	defer r.Body.Close()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		returnErrorCode(w, http.StatusBadRequest, "Service cannot read Request Body")
		return
	}

	err = json.Unmarshal(body, &servers)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	if servers.Action == "os-start" {
		actionFunc = context.restartInstance
		statusFilter = payloads.ComputeStatusStopped
	} else if servers.Action == "os-stop" {
		actionFunc = context.stopInstance
		statusFilter = payloads.ComputeStatusRunning
	} else if servers.Action == "os-delete" {
		actionFunc = context.deleteInstance
		statusFilter = ""
	} else {
		returnErrorCode(w, http.StatusServiceUnavailable, "Unsupported action")
		return
	}

	if len(servers.ServerIDs) > 0 {
		for _, instanceID := range servers.ServerIDs {
			// make sure the instance belongs to the tenant
			instance, err := context.ds.GetInstance(instanceID)

			if err != nil {
				returnErrorCode(w, http.StatusNotFound, "Instance %s could not be found", instanceID)
				return
			}

			if instance.TenantID != tenant {
				returnErrorCode(w, http.StatusNotFound, "Instance %s does not belong to tenant %s", instanceID, tenant)
				return
			}
			actionFunc(instanceID)
		}
	} else {
		/* We want to act on all relevant instances */
		instances, err := context.ds.GetAllInstancesFromTenant(tenant)
		if err != nil {
			returnErrorCode(w, http.StatusNotFound, "No instances for tenant")
			return
		}

		for _, instance := range instances {
			if statusFilter != "" && instance.State != statusFilter {
				continue
			}

			actionFunc(instance.ID)
		}
	}

	w.WriteHeader(http.StatusAccepted)
}

// @Title serverAction
// @Description Runs the indicated action (os-start, os-stop, os-delete) in the a server.
// @Accept  json
// @Success 202 {object} string "This operation does not return a response body, returns the 202 StatusAccepted code."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/{tenant}/servers/{server}/action [post]
// @Resource /v2.1/{tenant}/servers
func serverAction(w http.ResponseWriter, r *http.Request, context *controller) {
	vars := mux.Vars(r)
	tenant := vars["tenant"]
	instance := vars["server"]
	var action action

	dumpRequestBody(r, true)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	/* First check that the instance belongs to this tenant */
	i, err := context.ds.GetInstance(instance)
	if err != nil {
		returnErrorCode(w, http.StatusNotFound, "Instance could not be found")
		return
	}

	if i.TenantID != tenant {
		returnErrorCode(w, http.StatusNotFound, "Instance does not belong to tenant")
		return
	}

	defer r.Body.Close()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		returnErrorCode(w, http.StatusBadRequest, "Service cannot read Request Body")
		return
	}

	bodyString := string(body)

	if strings.Contains(bodyString, "os-start") {
		action = computeActionStart
	} else if strings.Contains(bodyString, "os-stop") {
		action = computeActionStop
	} else {
		returnErrorCode(w, http.StatusServiceUnavailable, "Unsupported action")
		return
	}

	switch action {
	case computeActionStart:
		err = context.restartInstance(instance)
	case computeActionStop:
		err = context.stopInstance(instance)
	}

	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// @Title listTenants
// @Description List all tenants.
// @Accept  json
// @Success 200 {array} interface "Marshalled format of payloads.CiaoComputeTenants representing the list of all tentants."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/tenants [get]
// @Resource /v2.1/tenants
func listTenants(w http.ResponseWriter, r *http.Request, context *controller) {
	var computeTenants payloads.CiaoComputeTenants

	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	tenants, err := context.ds.GetAllTenants()
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, tenant := range tenants {
		computeTenants.Tenants = append(computeTenants.Tenants,
			struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{
				ID:   tenant.ID,
				Name: tenant.Name,
			},
		)
	}

	b, err := json.Marshal(computeTenants)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title listNodes
// @Description Returns a list of all nodes.
// @Accept  json
// @Success 200 {array} interface "Returns ciao-controller.nodePager with TotalInstances, TotalRunningInstances, TotalPendingInstances, TotalPausedInstances."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/nodes [get]
// @Resource /v2.1/nodes
func listNodes(w http.ResponseWriter, r *http.Request, context *controller) {
	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	computeNodes := context.ds.GetNodeLastStats()

	nodeSummary, err := context.ds.GetNodeSummary()
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, node := range nodeSummary {
		for i := range computeNodes.Nodes {
			if computeNodes.Nodes[i].ID != node.NodeID {
				continue
			}

			computeNodes.Nodes[i].TotalInstances = node.TotalInstances
			computeNodes.Nodes[i].TotalRunningInstances = node.TotalRunningInstances
			computeNodes.Nodes[i].TotalPendingInstances = node.TotalPendingInstances
			computeNodes.Nodes[i].TotalPausedInstances = node.TotalPausedInstances
		}
	}

	sort.Sort(types.SortedComputeNodesByID(computeNodes.Nodes))

	pager := nodePager{
		context: context,
		nodes:   computeNodes.Nodes,
	}

	b, err := pager.nextPage(none, "", r)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title nodesSummary
// @Description A summary of all node stats.
// @Accept  json
// @Success 200 {object} interface "Returns payloads.CiaoClusterStatus with TotalNodesReady, TotalNodesFull, TotalNodesOffline and TotalNodesMaintenance."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/nodes/summary [get]
// @Resource /v2.1/nodes
func nodesSummary(w http.ResponseWriter, r *http.Request, context *controller) {
	var nodesStatus payloads.CiaoClusterStatus

	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	computeNodes := context.ds.GetNodeLastStats()

	glog.V(2).Infof("nodesSummary %d nodes", len(computeNodes.Nodes))

	nodesStatus.Status.TotalNodes = len(computeNodes.Nodes)
	for _, node := range computeNodes.Nodes {
		if node.Status == ssntp.READY.String() {
			nodesStatus.Status.TotalNodesReady++
		} else if node.Status == ssntp.FULL.String() {
			nodesStatus.Status.TotalNodesFull++
		} else if node.Status == ssntp.OFFLINE.String() {
			nodesStatus.Status.TotalNodesOffline++
		} else if node.Status == ssntp.MAINTENANCE.String() {
			nodesStatus.Status.TotalNodesMaintenance++
		}
	}

	b, err := json.Marshal(nodesStatus)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title serverAction
// @Description Runs the indicated action (os-start, os-stop, os-delete) in a server.
// @Accept  json
// @Success 202 {object} string "This operation does not return a response body, returns the 202 StatusAccepted code."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/nodes/{node}/servers/detail [get]
// @Resource /v2.1/nodes
func listNodeServers(w http.ResponseWriter, r *http.Request, context *controller) {
	vars := mux.Vars(r)
	nodeID := vars["node"]

	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	serversStats := context.ds.GetInstanceLastStats(nodeID)

	instances, err := context.ds.GetAllInstancesByNode(nodeID)
	if err != nil {
		returnErrorCode(w, http.StatusNotFound, "Instances could not be found in node")
		return
	}

	for _, instance := range instances {
		for i := range serversStats.Servers {
			if serversStats.Servers[i].ID != instance.ID {
				continue
			}

			serversStats.Servers[i].TenantID = instance.TenantID
			serversStats.Servers[i].IPv4 = instance.IPAddress
		}
	}

	pager := nodeServerPager{
		context:   context,
		instances: serversStats.Servers,
	}

	b, err := pager.nextPage(none, "", r)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title listCNCIs
// @Description Lists all CNCI agents.
// @Accept  json
// @Success 200 {array} payloads.CiaoCNCIs "Returns all CNCI agents data as InstanceId, TenantID, IPv4 and subnets."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/cncis [get]
// @Resource /v2.1/cncis
func listCNCIs(w http.ResponseWriter, r *http.Request, context *controller) {
	var ciaoCNCIs payloads.CiaoCNCIs

	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	cncis, err := context.ds.GetTenantCNCISummary("")
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	var subnets []payloads.CiaoCNCISubnet

	for _, cnci := range cncis {
		if cnci.InstanceID == "" {
			continue
		}

		for _, subnet := range cnci.Subnets {
			subnets = append(subnets,
				payloads.CiaoCNCISubnet{
					Subnet: subnet,
				},
			)
		}

		ciaoCNCIs.CNCIs = append(ciaoCNCIs.CNCIs,
			payloads.CiaoCNCI{
				ID:       cnci.InstanceID,
				TenantID: cnci.TenantID,
				IPv4:     cnci.IPAddress,
				Subnets:  subnets,
			},
		)
	}

	b, err := json.Marshal(ciaoCNCIs)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title listCNCIDetails
// @Description List details of a CNCI agent.
// @Accept  json
// @Success 200 {array} payloads.CiaoCNCIs "Returns details of a CNCI agent as InstanceId, TenantID, IPv4 and subnets."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/cncis/{cnci}/detail [get]
// @Resource /v2.1/cncis
func listCNCIDetails(w http.ResponseWriter, r *http.Request, context *controller) {
	vars := mux.Vars(r)
	cnciID := vars["cnci"]
	var ciaoCNCI payloads.CiaoCNCI

	dumpRequest(r)

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	cncis, err := context.ds.GetTenantCNCISummary(cnciID)
	if err != nil {
		returnErrorCode(w, http.StatusNotFound, "CNCI could not be found")
		return
	}

	if len(cncis) > 0 {
		var subnets []payloads.CiaoCNCISubnet
		cnci := cncis[0]

		for _, subnet := range cnci.Subnets {
			subnets = append(subnets,
				payloads.CiaoCNCISubnet{
					Subnet: subnet,
				},
			)
		}

		ciaoCNCI = payloads.CiaoCNCI{
			ID:       cnci.InstanceID,
			TenantID: cnci.TenantID,
			IPv4:     cnci.IPAddress,
			Subnets:  subnets,
		}
	}

	b, err := json.Marshal(ciaoCNCI)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title listTraces
// @Description List all Traces.
// @Accept  json
// @Success 200 {array} payloads.CiaoTracesSummary "Returns a summary of each trace in the system."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/traces [get]
// @Resource /v2.1/traces
func listTraces(w http.ResponseWriter, r *http.Request, context *controller) {
	var traces payloads.CiaoTracesSummary

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	summaries, err := context.ds.GetBatchFrameSummary()
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, s := range summaries {
		summary := payloads.CiaoTraceSummary{
			Label:     s.BatchID,
			Instances: s.NumInstances,
		}
		traces.Summaries = append(traces.Summaries, summary)
	}

	b, err := json.Marshal(traces)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title listEvents
// @Description List all Events.
// @Accept  json
// @Success 200 {array} payloads.CiaoEvent "Returns all events from the log system."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/events [get]
// @Resource /v2.1/events
func listEvents(w http.ResponseWriter, r *http.Request, context *controller) {
	vars := mux.Vars(r)
	tenant := vars["tenant"]

	events := payloads.NewCiaoEvents()

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	logs, err := context.ds.GetEventLog()
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, l := range logs {
		if tenant != "" && tenant != l.TenantID {
			continue
		}

		event := payloads.CiaoEvent{
			Timestamp: l.Timestamp,
			TenantID:  l.TenantID,
			EventType: l.EventType,
			Message:   l.Message,
		}
		events.Events = append(events.Events, event)
	}

	b, err := json.Marshal(events)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// @Title listTenantEvents
// @Description List Events.
// @Accept  json
// @Success 200 {array} payloads.CiaoEvent "Returns the events of a tenant from the log system."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/{tenant}/events [get]
// @Resource /v2.1/events
// listTenantEvents is created with the only purpose of API documentation for method
// /v2.1/{tenant}/events
func listTenantEvents(w http.ResponseWriter, r *http.Request, context *controller) {
	listEvents(w, r, context)
}

// @Title clearEvents
// @Description Clear Events Log.
// @Accept  json
// @Success 202 {object} string "This operation does not return a response body, returns the 202 StatusAccepted code."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/events [delete]
// @Resource /v2.1/events
func clearEvents(w http.ResponseWriter, r *http.Request, context *controller) {
	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	err := context.ds.ClearLog()
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// @Title traceData
// @Description Trace data of a indicated trace.
// @Accept json
// @Success 200 {array} payloads.CiaoBatchFrameStat "Returns a summary of a trace in the system."
// @Failure 400 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 40x corresponding code."
// @Failure 500 {object} payloads.HTTPReturnErrorCode "The response contains the corresponding message and 50x corresponding code."
// @Router /v2.1/traces/{label} [get]
// @Resource /v2.1/traces
func traceData(w http.ResponseWriter, r *http.Request, context *controller) {
	vars := mux.Vars(r)
	label := vars["label"]
	var traceData payloads.CiaoTraceData

	if validateToken(context, r) == false {
		returnErrorCode(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	batchStats, err := context.ds.GetBatchFrameStatistics(label)
	if err != nil {
		returnErrorCode(w, http.StatusNotFound, "Could not found trace with label")
		return
	}

	traceData.Summary = payloads.CiaoBatchFrameStat{
		NumInstances:             batchStats[0].NumInstances,
		TotalElapsed:             batchStats[0].TotalElapsed,
		AverageElapsed:           batchStats[0].AverageElapsed,
		AverageControllerElapsed: batchStats[0].AverageControllerElapsed,
		AverageLauncherElapsed:   batchStats[0].AverageLauncherElapsed,
		AverageSchedulerElapsed:  batchStats[0].AverageSchedulerElapsed,
		VarianceController:       batchStats[0].VarianceController,
		VarianceLauncher:         batchStats[0].VarianceLauncher,
		VarianceScheduler:        batchStats[0].VarianceScheduler,
	}

	b, err := json.Marshal(traceData)
	if err != nil {
		returnErrorCode(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func createComputeAPI(context *controller) {
	r := mux.NewRouter()

	r.HandleFunc("/v2.1/{tenant}/servers", func(w http.ResponseWriter, r *http.Request) {
		createServer(w, r, context)
	}).Methods("POST")

	r.HandleFunc("/v2.1/{tenant}/servers/detail", func(w http.ResponseWriter, r *http.Request) {
		listServerDetails(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/{tenant}/servers/{server}", func(w http.ResponseWriter, r *http.Request) {
		showServerDetails(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/{tenant}/servers/{server}", func(w http.ResponseWriter, r *http.Request) {
		deleteServer(w, r, context)
	}).Methods("DELETE")

	r.HandleFunc("/v2.1/{tenant}/servers/action", func(w http.ResponseWriter, r *http.Request) {
		tenantServersAction(w, r, context)
	}).Methods("POST")

	r.HandleFunc("/v2.1/{tenant}/servers/{server}/action", func(w http.ResponseWriter, r *http.Request) {
		serverAction(w, r, context)
	}).Methods("POST")

	r.HandleFunc("/v2.1/{tenant}/flavors", func(w http.ResponseWriter, r *http.Request) {
		listFlavors(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/{tenant}/flavors/detail", func(w http.ResponseWriter, r *http.Request) {
		listFlavorsDetails(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/{tenant}/flavors/{flavor}", func(w http.ResponseWriter, r *http.Request) {
		showFlavorDetails(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/{tenant}/resources", func(w http.ResponseWriter, r *http.Request) {
		listTenantResources(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/{tenant}/quotas", func(w http.ResponseWriter, r *http.Request) {
		listTenantQuotas(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/{tenant}/events", func(w http.ResponseWriter, r *http.Request) {
		listTenantEvents(w, r, context)
	}).Methods("GET")

	/* Avoid conflict with {tenant}/servers/detail */
	r.HandleFunc("/v2.1/nodes/{node}/servers/detail", func(w http.ResponseWriter, r *http.Request) {
		listNodeServers(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/flavors/{flavor}/servers/detail", func(w http.ResponseWriter, r *http.Request) {
		listFlavorServerDetail(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/tenants", func(w http.ResponseWriter, r *http.Request) {
		listTenants(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/nodes", func(w http.ResponseWriter, r *http.Request) {
		listNodes(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/nodes/summary", func(w http.ResponseWriter, r *http.Request) {
		nodesSummary(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/cncis", func(w http.ResponseWriter, r *http.Request) {
		listCNCIs(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/cncis/{cnci}/detail", func(w http.ResponseWriter, r *http.Request) {
		listCNCIDetails(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/events", func(w http.ResponseWriter, r *http.Request) {
		listEvents(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/events", func(w http.ResponseWriter, r *http.Request) {
		clearEvents(w, r, context)
	}).Methods("DELETE")

	r.HandleFunc("/v2.1/traces", func(w http.ResponseWriter, r *http.Request) {
		listTraces(w, r, context)
	}).Methods("GET")

	r.HandleFunc("/v2.1/traces/{label}", func(w http.ResponseWriter, r *http.Request) {
		traceData(w, r, context)
	}).Methods("GET")

	service := fmt.Sprintf(":%d", computeAPIPort)
	log.Fatal(http.ListenAndServeTLS(service, httpsCAcert, httpsKey, r))
}
