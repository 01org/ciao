//
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
//

package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"

	"github.com/01org/ciao/payloads"
)

var scopedToken string

const openstackComputePort = 8774
const openstackComputeVersion = "v2.1"

type action uint8

const (
	computeActionStart action = iota
	computeActionStop
)

func debugf(format string, args ...interface{}) {
	glog.V(2).Infof("ciao-cli DEBUG: "+format, args...)
}

func infof(format string, args ...interface{}) {
	glog.V(1).Infof("ciao-cli INFO: "+format, args...)
}

func warningf(format string, args ...interface{}) {
	glog.Warningf("ciao-cli WARNING: "+format, args...)
}

func errorf(format string, args ...interface{}) {
	glog.Errorf("ciao-cli ERROR: "+format, args...)
}

func fatalf(format string, args ...interface{}) {
	glog.Fatalf("ciao-cli FATAL: "+format, args...)
	os.Exit(1)
}

var (
	allInstances     = flag.Bool("all-instances", false, "Select all instances")
	instanceLabel    = flag.String("instance-label", "", "Set a frame label. This will trigger frame tracing")
	listInstances    = flag.Bool("list-instances", false, "List all instances for a tenant")
	listCNInstances  = flag.Bool("list-cn-instances", false, "List all instances for a compute node")
	listWlInstances  = flag.Bool("list-wl-instances", false, "List all instances for a workload")
	listQuotas       = flag.Bool("list-quotas", false, "List quotas status for a tenant")
	listResources    = flag.Bool("list-resources", false, "List consumed resources for a tenant for the past 15mn")
	listWorkloads    = flag.Bool("list-workloads", false, "List all workloads")
	listUserTenants  = flag.Bool("list-tenants", false, "List all tenants for a given user")
	listTenants      = flag.Bool("list-all-tenants", false, "List all tenants")
	listComputeNodes = flag.Bool("list-cns", false, "List all compute nodes")
	listCNCIs        = flag.Bool("list-cncis", false, "List all CNCIs")
	listLength       = flag.Int("list-length", 0, "Maximum number of items in the reponse")
	listLabels       = flag.Bool("list-labels", false, "List all trace labels")
	listAllEvents    = flag.Bool("list-all-events", false, "List all cluster events")
	listEvents       = flag.Bool("list-events", false, "List all events for a tenant")
	dumpCNCI         = flag.Bool("dump-cnci", false, "Dump a CNCI details")
	dumpToken        = flag.Bool("dump-token", false, "Dump keystone tokens")
	clusterStatus    = flag.Bool("cluster-status", false, "List all compute nodes")
	launchInstances  = flag.Bool("launch-instances", false, "Launch Ciao instances")
	deleteInstance   = flag.Bool("delete-instance", false, "Delete a Ciao instance")
	deleteEvents     = flag.Bool("delete-events", false, "Delete all stored Ciao events")
	stopInstance     = flag.Bool("stop-instance", false, "Stop a Ciao instance")
	restartInstance  = flag.Bool("restart-instance", false, "Restart a Ciao instance")
	workload         = flag.String("workload", "", "Workload UUID")
	instances        = flag.Int("instances", 1, "Number of instances to create")
	instance         = flag.String("instance", "", "Instance UUID")
	instanceMarker   = flag.String("instance-marker", "", "Show instance list starting from the next instance after instance-marker")
	instanceOffset   = flag.Int("instance-offset", 0, "Show instance list starting from instance #instance-offset")
	tenantID         = flag.String("tenant-id", "", "Tenant UUID")
	tenantName       = flag.String("tenant-name", "", "Tenant name")
	computeNode      = flag.String("cn", "", "Compute node UUID")
	cnci             = flag.String("cnci", "", "CNCI UUID")
	controllerURL    = flag.String("controller", "", "Controller URL")
	computePort      = flag.Int("computeport", openstackComputePort, "Openstack Compute API port")
	identityURL      = flag.String("identity", "", "Keystone URL")
	identityUser     = flag.String("username", "", "Openstack Service Username")
	identityPassword = flag.String("password", "", "Openstack Service Username")
	dumpLabel        = flag.String("dump-label", "", "Dump all trace data for a given label")
)

const (
	ciaoIdentityEnv    = "CIAO_IDENTITY"
	ciaoControllerEnv  = "CIAO_CONTROLLER"
	ciaoUsernameEnv    = "CIAO_USERNAME"
	ciaoPasswordEnv    = "CIAO_PASSWORD"
	ciaoComputePortEnv = "CIAO_COMPUTEPORT"
)

type queryValue struct {
	name, value string
}

func buildComputeURL(format string, args ...interface{}) string {
	prefix := fmt.Sprintf("https://%s:%d/%s/", *controllerURL, *computePort, openstackComputeVersion)
	return fmt.Sprintf(prefix+format, args...)
}

func sendHTTPRequestToken(method string, url string, values []queryValue, token string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, os.ExpandEnv(url), body)
	if err != nil {
		return nil, err
	}

	infof("Sending %s %s\n", method, url)

	if values != nil {
		v := req.URL.Query()

		for _, value := range values {
			infof("Adding URL query %s=%s\n", value.name, value.value)
			v.Add(value.name, value.value)
		}

		req.URL.RawQuery = v.Encode()
	}

	if token != "" {
		req.Header.Add("X-Auth-Token", token)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
	}

	warningf("Skipping TLS verification\n")
	tlsConfig := &tls.Config{InsecureSkipVerify: true}

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	client := &http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		errorf("Could not send HTTP request %s\n", err)
		return nil, err
	}

	infof("Got HTTP response (status %s)\n", resp.Status)

	if resp.StatusCode >= http.StatusBadRequest {
		respBody, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			errorf("Could not read the HTTP response %s\n", err)
			spew.Dump(resp.Body)
			return resp, err
		}

		return resp, fmt.Errorf("HTTP Error [%d] for [%s %s]: %s", resp.StatusCode, method, url, respBody)
	}

	return resp, err
}

func sendHTTPRequest(method string, url string, values []queryValue, body io.Reader) (*http.Response, error) {
	return sendHTTPRequestToken(method, url, values, scopedToken, body)
}

func unmarshalHTTPResponse(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		errorf("Could not read the HTTP response %s\n", err)
		return err
	}

	err = json.Unmarshal(body, v)
	if err != nil {
		errorf("Could not unmarshal the HTTP response %s\n", err)
		return err
	}

	if glog.V(2) {
		spew.Dump(v)
	}

	return nil
}

func listAllInstances(tenant string, workload string, marker string, offset int, limit int) {
	var servers payloads.ComputeServers
	var url string

	if *listInstances == true && tenant == "" {
		fatalf("Missing required -tenant-id parameter")
	}

	if *listWlInstances == true && workload == "" {
		fatalf("Missing required -workload parameter")
	}

	if tenant != "" {
		url = buildComputeURL("%s/servers/detail", tenant)
	} else if workload != "" {
		url = buildComputeURL("flavors/%s/servers/detail", workload)
	}

	var values []queryValue
	if limit > 0 {
		values = append(values, queryValue{
			name:  "limit",
			value: fmt.Sprintf("%d", limit),
		})
	}

	if offset > 0 {
		values = append(values, queryValue{
			name:  "offset",
			value: fmt.Sprintf("%d", offset),
		})
	}

	if marker != "" {
		values = append(values, queryValue{
			name:  "marker",
			value: marker,
		})
	}

	resp, err := sendHTTPRequest("GET", url, values, nil)
	if err != nil {
		fatalf(err.Error())
	}

	err = unmarshalHTTPResponse(resp, &servers)
	if err != nil {
		fatalf(err.Error())
	}

	for i, server := range servers.Servers {
		fmt.Printf("Instance #%d\n", i+1)
		fmt.Printf("\tUUID: %s\n", server.ID)
		fmt.Printf("\tStatus: %s\n", server.Status)
		fmt.Printf("\tPrivate IP: %s\n", server.Addresses.Private[0].Addr)
		fmt.Printf("\tMAC Address: %s\n", server.Addresses.Private[0].OSEXTIPSMACMacAddr)
		fmt.Printf("\tCN UUID: %s\n", server.HostID)
		fmt.Printf("\tImage UUID: %s\n", server.Image.ID)
		fmt.Printf("\tTenant UUID: %s\n", server.TenantID)
		if server.SSHIP != "" {
			fmt.Printf("\tSSH IP: %s\n", server.SSHIP)
			fmt.Printf("\tSSH Port: %d\n", server.SSHPort)
		}
	}
}

func limitToString(limit int) string {
	if limit == -1 {
		return "Unlimited"
	}

	return fmt.Sprintf("%d", limit)
}

func listTenantQuotas(tenant string) {
	if tenant == "" {
		fatalf("Missing required -tenant-id parameter")
	}

	var resources payloads.CiaoTenantResources
	url := buildComputeURL("%s/quotas", tenant)

	resp, err := sendHTTPRequest("GET", url, nil, nil)
	if err != nil {
		fatalf(err.Error())
	}

	err = unmarshalHTTPResponse(resp, &resources)
	if err != nil {
		fatalf(err.Error())
	}

	fmt.Printf("Quotas for tenant %s:\n", resources.ID)
	fmt.Printf("\tInstances: %d | %s\n", resources.InstanceUsage, limitToString(resources.InstanceLimit))
	fmt.Printf("\tCPUs:      %d | %s\n", resources.VCPUUsage, limitToString(resources.VCPULimit))
	fmt.Printf("\tMemory:    %d | %s\n", resources.MemUsage, limitToString(resources.MemLimit))
	fmt.Printf("\tDisk:      %d | %s\n", resources.DiskUsage, limitToString(resources.DiskLimit))
}

func listTenantResources(tenant string) {
	if tenant == "" {
		fatalf("Missing required -tenant-id parameter")
	}

	var usage payloads.CiaoUsageHistory
	url := buildComputeURL("%s/resources", tenant)

	now := time.Now()
	values := []queryValue{
		{
			name:  "start_date",
			value: now.Add(-15 * time.Minute).Format(time.RFC3339),
		},
		{
			name:  "end_date",
			value: now.Format(time.RFC3339),
		},
	}

	resp, err := sendHTTPRequest("GET", url, values, nil)
	if err != nil {
		fatalf(err.Error())
	}

	err = unmarshalHTTPResponse(resp, &usage)
	if err != nil {
		fatalf(err.Error())
	}

	if len(usage.Usages) == 0 {
		fmt.Printf("No usage history for %s\n", tenant)
		return
	}

	fmt.Printf("Usage for tenant %s:\n", tenant)
	for _, u := range usage.Usages {
		fmt.Printf("\t%v: [%d CPUs] [%d MB memory] [%d MB disk]\n", u.Timestamp, u.VCPU, u.Memory, u.Disk)
	}
}

func listTenantWorkloads(tenant string) {
	if tenant == "" {
		fatalf("Missing required -tenant-id parameter")
	}

	var flavors payloads.ComputeFlavorsDetails
	if tenant == "" {
		tenant = "faketenant"
	}

	url := buildComputeURL("%s/flavors/detail", tenant)

	resp, err := sendHTTPRequest("GET", url, nil, nil)
	if err != nil {
		fatalf(err.Error())
	}

	err = unmarshalHTTPResponse(resp, &flavors)
	if err != nil {
		fatalf(err.Error())
	}

	for i, flavor := range flavors.Flavors {
		fmt.Printf("Workload %d\n", i+1)
		fmt.Printf("\tName: %s\n\tUUID:%s\n\tImage UUID: %s\n\tCPUs: %d\n\tMemory: %d MB\n",
			flavor.Name, flavor.ID, flavor.Disk, flavor.Vcpus, flavor.RAM)
	}
}

func listAllTenants() {
	projects, err := getAllProjects(*identityUser, *identityPassword)
	if err != nil {
		fatalf(err.Error())
	}

	for i, project := range projects.Projects {
		fmt.Printf("Tenant [%d]\n", i+1)
		fmt.Printf("\tUUID: %s\n", project.ID)
		fmt.Printf("\tName: %s\n", project.Name)
	}
}

func listUserSpecificTenants(username, password string) {
	projects, err := getUserProjects(username, password)
	if err != nil {
		fatalf(err.Error())
	}

	fmt.Printf("Projects for user %s\n", username)
	for _, project := range projects {
		fmt.Printf("\tUUID: %s\n", project.ID)
		fmt.Printf("\tName: %s\n", project.Name)
	}
}

func listAllComputeNodes() {
	var nodes payloads.CiaoComputeNodes

	url := buildComputeURL("nodes")

	resp, err := sendHTTPRequest("GET", url, nil, nil)
	if err != nil {
		fatalf(err.Error())
	}

	err = unmarshalHTTPResponse(resp, &nodes)
	if err != nil {
		fatalf(err.Error())
	}

	for i, node := range nodes.Nodes {
		fmt.Printf("Compute Node %d\n", i+1)
		fmt.Printf("\tUUID: %s\n", node.ID)
		fmt.Printf("\tStatus: %s\n", node.Status)
		fmt.Printf("\tLoad: %d\n", node.Load)
		fmt.Printf("\tAvailable/Total memory: %d/%d MB\n", node.MemAvailable, node.MemTotal)
		fmt.Printf("\tAvailable/Total disk: %d/%d MB\n", node.DiskAvailable, node.DiskTotal)
		fmt.Printf("\tTotal Instances: %d\n", node.TotalInstances)
		fmt.Printf("\t\tRunning Instances: %d\n", node.TotalRunningInstances)
		fmt.Printf("\t\tPending Instances: %d\n", node.TotalPendingInstances)
		fmt.Printf("\t\tPaused Instances: %d\n", node.TotalPausedInstances)
	}
}

func listAllCNCIs() {
	var cncis payloads.CiaoCNCIs

	url := buildComputeURL("cncis")

	resp, err := sendHTTPRequest("GET", url, nil, nil)
	if err != nil {
		fatalf(err.Error())
	}

	err = unmarshalHTTPResponse(resp, &cncis)
	if err != nil {
		fatalf(err.Error())
	}

	for i, cnci := range cncis.CNCIs {
		fmt.Printf("CNCI %d\n", i+1)
		fmt.Printf("\tCNCI UUID: %s\n", cnci.ID)
		fmt.Printf("\tTenant UUID: %s\n", cnci.TenantID)
		fmt.Printf("\tIPv4: %s\n", cnci.IPv4)
		fmt.Printf("\tSubnets:\n")
		for _, subnet := range cnci.Subnets {
			fmt.Printf("\t\t%s\n", subnet.Subnet)
		}
	}
}

func dumpCNCIDetails(cnciID string) {
	if cnciID == "" {
		fatalf("Missing required -cnci parameter")
	}

	var cnci payloads.CiaoCNCI

	url := buildComputeURL("cncis/%s/detail", cnciID)

	resp, err := sendHTTPRequest("GET", url, nil, nil)
	if err != nil {
		fatalf(err.Error())
	}

	err = unmarshalHTTPResponse(resp, &cnci)
	if err != nil {
		fatalf(err.Error())
	}

	fmt.Printf("\tCNCI UUID: %s\n", cnci.ID)
	fmt.Printf("\tTenant UUID: %s\n", cnci.TenantID)
	fmt.Printf("\tIPv4: %s\n", cnci.IPv4)
	fmt.Printf("\tSubnets:\n")
	for _, subnet := range cnci.Subnets {
		fmt.Printf("\t\t%s\n", subnet.Subnet)
	}
}

func createTenantInstance(tenant string, workload string, instances int, label string) {
	if tenant == "" {
		fatalf("Missing required -tenant-id parameter")
	}

	if workload == "" {
		fatalf("Missing required -workload parameter")
	}

	var server payloads.ComputeCreateServer
	var servers payloads.ComputeServers

	server.Server.Name = label
	server.Server.Workload = workload
	server.Server.MaxInstances = instances
	server.Server.MinInstances = 1

	serverBytes, err := json.Marshal(server)
	if err != nil {
		fatalf(err.Error())
	}
	body := bytes.NewReader(serverBytes)

	url := buildComputeURL("%s/servers", tenant)

	resp, err := sendHTTPRequest("POST", url, nil, body)
	if err != nil {
		fatalf(err.Error())
	}

	if resp.StatusCode != http.StatusAccepted {
		fatalf("Instance creation failed: %s", resp.Status)
	}

	err = unmarshalHTTPResponse(resp, &servers)
	if err != nil {
		fatalf(err.Error())
	}

	for _, server := range servers.Servers {
		fmt.Printf("Created new instance: %s\n", server.ID)
	}
}

func deleteTenantInstance(tenant string, instance string) {
	url := buildComputeURL("%s/servers/%s", tenant, instance)

	resp, err := sendHTTPRequest("DELETE", url, nil, nil)
	if err != nil {
		fatalf(err.Error())

	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		fatalf("Instance deletion failed: %s", resp.Status)
	}

	fmt.Printf("Deleted instance: %s\n", instance)
}

func actionAllTenantInstance(tenant string, osAction string) {
	var action payloads.CiaoServersAction

	url := buildComputeURL("%s/servers/action", tenant)

	action.Action = osAction

	actionBytes, err := json.Marshal(action)
	if err != nil {
		fatalf(err.Error())
	}

	body := bytes.NewReader(actionBytes)

	resp, err := sendHTTPRequest("POST", url, nil, body)
	if err != nil {
		fatalf(err.Error())

	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		fatalf("Action %s on all instances failed: %s", osAction, resp.Status)
	}

	fmt.Printf("%s all instances for tenant %s\n", osAction, tenant)
}

func actionTenantInstance(tenant string, instance string, osAction string) {
	if tenant == "" {
		fatalf("Missing required -tenant-id parameter")
	}

	if instance == "" {
		actionAllTenantInstance(tenant, osAction)
	} else if osAction == osDelete {
		deleteTenantInstance(tenant, instance)
	} else {
		fatalf("Unsupported action %s for instance %s", osAction, instance)
	}
}

func listNodeInstances(node string) {
	if node == "" {
		fatalf("Missing required -cn parameter")
	}

	var servers payloads.CiaoServersStats
	url := buildComputeURL("nodes/%s/servers/detail", node)

	resp, err := sendHTTPRequest("GET", url, nil, nil)
	if err != nil {
		fatalf(err.Error())
	}

	err = unmarshalHTTPResponse(resp, &servers)
	if err != nil {
		fatalf(err.Error())
	}

	for i, server := range servers.Servers {
		fmt.Printf("Instance #%d\n", i+1)
		fmt.Printf("\tUUID: %s\n", server.ID)
		fmt.Printf("\tStatus: %s\n", server.Status)
		fmt.Printf("\tTenant UUID: %s\n", server.TenantID)
		fmt.Printf("\tIPv4: %s\n", server.IPv4)
		fmt.Printf("\tCPUs used: %d\n", server.VCPUUsage)
		fmt.Printf("\tMemory used: %d MB\n", server.MemUsage)
		fmt.Printf("\tDisk used: %d MB\n", server.DiskUsage)
	}
}

func dumpClusterStatus() {
	var status payloads.CiaoClusterStatus
	url := buildComputeURL("nodes/summary")

	resp, err := sendHTTPRequest("GET", url, nil, nil)
	if err != nil {
		fatalf(err.Error())
	}

	err = unmarshalHTTPResponse(resp, &status)
	if err != nil {
		fatalf(err.Error())
	}

	fmt.Printf("Total Nodes %d\n", status.Status.TotalNodes)
	fmt.Printf("\tReady %d\n", status.Status.TotalNodesReady)
	fmt.Printf("\tFull %d\n", status.Status.TotalNodesFull)
	fmt.Printf("\tOffline %d\n", status.Status.TotalNodesOffline)
	fmt.Printf("\tMaintenance %d\n", status.Status.TotalNodesMaintenance)
}

const (
	osStart  = "os-start"
	osStop   = "os-stop"
	osDelete = "os-delete"
)

func startStopInstance(tenant, instance string, stop bool) {
	if tenant == "" {
		fatalf("Missing required -tenant-id parameter")
	}

	if instance == "" {
		fatalf("Missing required -instance parameter")
	}

	actionBytes := []byte(osStart)
	if stop == true {
		actionBytes = []byte(osStop)
	}

	body := bytes.NewReader(actionBytes)

	url := buildComputeURL("%s/servers/%s/action", tenant, instance)

	resp, err := sendHTTPRequest("POST", url, nil, body)
	if err != nil {
		fatalf(err.Error())
	}

	if resp.StatusCode != http.StatusAccepted {
		fatalf("Instance action failed: %s", resp.Status)
	}

	if stop == true {
		fmt.Printf("Instance %s stopped\n", instance)
	} else {
		fmt.Printf("Instance %s restarted\n", instance)
	}
}

func listAllLabels() {
	var traces payloads.CiaoTracesSummary

	url := buildComputeURL("traces")

	resp, err := sendHTTPRequest("GET", url, nil, nil)
	if err != nil {
		fatalf(err.Error())
	}

	err = unmarshalHTTPResponse(resp, &traces)
	if err != nil {
		fatalf(err.Error())
	}

	fmt.Printf("%d trace label(s) available\n", len(traces.Summaries))
	for i, summary := range traces.Summaries {
		fmt.Printf("\tLabel #%d: %s (%d instances running)\n", i+1, summary.Label, summary.Instances)
	}

}

func listClusterEvents(tenant string, all bool) {
	if all == false && tenant == "" {
		fatalf("Missing required -tenant-id parameter")
	}

	var events payloads.CiaoEvents
	var url string

	if all == true {
		url = buildComputeURL("events")
	} else {
		url = buildComputeURL("%s/events", tenant)
	}

	resp, err := sendHTTPRequest("GET", url, nil, nil)
	if err != nil {
		fatalf(err.Error())
	}

	err = unmarshalHTTPResponse(resp, &events)
	if err != nil {
		fatalf(err.Error())
	}

	fmt.Printf("%d Ciao event(s):\n", len(events.Events))
	for i, event := range events.Events {
		fmt.Printf("\t[%d] %v: %s:%s (Tenant %s)\n", i+1, event.Timestamp, event.EventType, event.Message, event.TenantID)
	}

}

func deleteAllEvents() {
	url := buildComputeURL("events")

	resp, err := sendHTTPRequest("DELETE", url, nil, nil)
	if err != nil {
		fatalf(err.Error())
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		fatalf("Events log deletion failed: %s", resp.Status)
	}

	fmt.Printf("Deleted all event logs\n")
}

func dumpTraceData(label string) {
	var traceData payloads.CiaoTraceData

	url := buildComputeURL("traces/%s", label)

	resp, err := sendHTTPRequest("GET", url, nil, nil)
	if err != nil {
		fatalf(err.Error())
	}

	err = unmarshalHTTPResponse(resp, &traceData)
	if err != nil {
		fatalf(err.Error())
	}

	fmt.Printf("Trace data for [%s]:\n", label)
	fmt.Printf("\tNumber of instances: %d\n", traceData.Summary.NumInstances)
	fmt.Printf("\tTotal time elapsed     : %f seconds\n", traceData.Summary.TotalElapsed)
	fmt.Printf("\tAverage time elapsed   : %f seconds\n", traceData.Summary.AverageElapsed)
	fmt.Printf("\tAverage Controller time: %f seconds\n", traceData.Summary.AverageControllerElapsed)
	fmt.Printf("\tAverage Scheduler time : %f seconds\n", traceData.Summary.AverageSchedulerElapsed)
	fmt.Printf("\tAverage Launcher time  : %f seconds\n", traceData.Summary.AverageLauncherElapsed)
	fmt.Printf("\tController variance    : %f seconds²\n", traceData.Summary.VarianceController)
	fmt.Printf("\tScheduler variance     : %f seconds²\n", traceData.Summary.VarianceScheduler)
	fmt.Printf("\tLauncher variance      : %f seconds²\n", traceData.Summary.VarianceLauncher)

}

func getCiaoEnvVariables() {
	identity := os.Getenv(ciaoIdentityEnv)
	controller := os.Getenv(ciaoControllerEnv)
	username := os.Getenv(ciaoUsernameEnv)
	password := os.Getenv(ciaoPasswordEnv)
	port := os.Getenv(ciaoComputePortEnv)

	infof("Ciao environment variables:\n")
	infof("\t%s:%s\n", ciaoIdentityEnv, identity)
	infof("\t%s:%s\n", ciaoControllerEnv, controller)
	infof("\t%s:%s\n", ciaoUsernameEnv, username)
	infof("\t%s:%s\n", ciaoPasswordEnv, password)
	infof("\t%s:%s\n", ciaoComputePortEnv, port)

	if identity != "" && *identityURL == "" {
		*identityURL = identity
	}

	if controller != "" && *controllerURL == "" {
		*controllerURL = controller
	}

	if username != "" && *identityUser == "" {
		*identityUser = username
	}

	if password != "" && *identityPassword == "" {
		*identityPassword = password
	}

	if port != "" && *computePort == openstackComputePort {
		*computePort, _ = strconv.Atoi(port)
	}

}

func checkCompulsoryOptions() {
	fatal := ""

	if *identityURL == "" {
		fatal += "Missing required identity URL\n"
	}

	if *identityUser == "" {
		fatal += "Missing required username\n"
	}

	if *identityPassword == "" {
		fatal += "Missing required password\n"
	}

	if *controllerURL == "" {
		fatal += "Missing required Ciao controller URL\n"
	}

	if *allInstances == true && *instance != "" {
		fatal += "All instances or one single instance ?\n"
	}

	if fatal != "" {
		fatalf(fatal)
	}
}

func cliList() {
	if *listInstances == true {
		listAllInstances(*tenantID, "", *instanceMarker, *instanceOffset, *listLength)
	}

	if *listWlInstances == true {
		listAllInstances("", *workload, *instanceMarker, *instanceOffset, *listLength)
	}

	if *listCNInstances == true {
		listNodeInstances(*computeNode)
	}

	if *listQuotas == true {
		listTenantQuotas(*tenantID)
	}

	if *listResources == true {
		listTenantResources(*tenantID)
	}

	if *listWorkloads == true {
		listTenantWorkloads(*tenantID)
	}

	if *listComputeNodes == true {
		listAllComputeNodes()
	}

	if *listCNCIs == true {
		listAllCNCIs()
	}

	if *listLabels == true {
		listAllLabels()
	}

	if *listEvents == true || *listAllEvents == true {
		listClusterEvents(*tenantID, *listAllEvents)
	}
}

func cliDump() {
	if *clusterStatus == true {
		dumpClusterStatus()
	}

	if *dumpCNCI == true {
		dumpCNCIDetails(*cnci)
	}

	if *dumpLabel != "" {
		dumpTraceData(*dumpLabel)
	}
}

func cliActionInstances() {
	if *launchInstances == true {
		createTenantInstance(*tenantID, *workload, *instances, *instanceLabel)
	}

	if *deleteInstance == true {
		actionTenantInstance(*tenantID, *instance, osDelete)
	}

	if *stopInstance == true || *restartInstance == true {
		startStopInstance(*tenantID, *instance, *stopInstance)
	}
}

func cliEvent() {
	if *deleteEvents == true {
		deleteAllEvents()
	}
}

func main() {
	var err error

	flag.Parse()

	getCiaoEnvVariables()
	checkCompulsoryOptions()

	/* First check if we're being asked for a tenants list */
	if *listTenants == true {
		listAllTenants()
		return
	}

	if *listUserTenants == true {
		listUserSpecificTenants(*identityUser, *identityPassword)
		return
	}

	/* If we're missing the tenant name let's try to fetch one */
	if *tenantName == "" {
		*tenantName, *tenantID, err = getTenant(*identityUser, *identityPassword, *tenantID)
		if err != nil {
			fatalf(err.Error())
		}

		warningf("Unspecified scope, using (%s, %s)", *tenantName, *tenantID)
	}

	scopedToken, *tenantID, _, err = getScopedToken(*identityUser, *identityPassword, *tenantName)
	if err != nil {
		fatalf(err.Error())
	}

	cliList()
	cliDump()
	cliActionInstances()
	cliEvent()
}
