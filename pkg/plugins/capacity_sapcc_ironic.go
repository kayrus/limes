/*******************************************************************************
*
* Copyright 2018 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package plugins

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	flavorsmodule "github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/pagination"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/limes"
	"github.com/sapcc/limes/pkg/core"
)

type capacitySapccIronicPlugin struct {
	cfg                 core.CapacitorConfiguration
	ftt                 novaFlavorTranslationTable
	reportSubcapacities bool
}

type capacitySapccIronicSerializedMetrics struct {
	UnmatchedNodeCount uint64 `json:"unmatched_nodes"`
}

func init() {
	core.RegisterCapacityPlugin(func(c core.CapacitorConfiguration, scrapeSubcapacities map[string]map[string]bool) core.CapacityPlugin {
		ftt := newNovaFlavorTranslationTable(c.SAPCCIronic.FlavorAliases)
		return &capacitySapccIronicPlugin{c, ftt, scrapeSubcapacities["compute"]["instances-baremetal"]}
	})
}

//Init implements the core.CapacityPlugin interface.
func (p *capacitySapccIronicPlugin) Init(provider *gophercloud.ProviderClient, eo gophercloud.EndpointOpts) error {
	return nil
}

//ID implements the core.CapacityPlugin interface.
func (p *capacitySapccIronicPlugin) ID() string {
	return "sapcc-ironic"
}

type ironicFlavorInfo struct {
	ID           string
	Name         string
	Cores        uint64
	MemoryMiB    uint64
	DiskGiB      uint64
	Capabilities map[string]string
}

//Reference:
//  Hosts are expected to be in one of the following format:
//    - "nova-compute-xxxx"
//    - "nova-compute-ironic-xxxx"
//  where "xxxx" is unique among all hosts.
var computeHostStubRx = regexp.MustCompile(`^nova-compute-(?:ironic-)?([a-zA-Z0-9]+)$`)

//Node names are expected to be in the form "nodeXXX-bmYYY" or "nodeXXX-bbYYY" or "nodeXXX-apYYY", where the second half is the host stub (the match group from above).
var nodeNameRx = regexp.MustCompile(`^node\d+-((?:b[bm]|ap)\d+)$`)

//Scrape implements the core.CapacityPlugin interface.
func (p *capacitySapccIronicPlugin) Scrape(provider *gophercloud.ProviderClient, eo gophercloud.EndpointOpts) (map[string]map[string]core.CapacityData, string, error) {
	//collect info about flavors with separate instance quota
	novaClient, err := openstack.NewComputeV2(provider, eo)
	if err != nil {
		return nil, "", err
	}
	flavors, err := p.collectIronicFlavorInfo(novaClient)
	if err != nil {
		return nil, "", err
	}

	//we are going to report capacity for all per-flavor instance quotas
	result := make(map[string]*core.CapacityData)
	for _, flavor := range flavors {
		result[p.ftt.LimesResourceNameForFlavor(flavor.Name)] = &core.CapacityData{
			Capacity:      0,
			CapacityPerAZ: map[string]*core.CapacityDataForAZ{},
		}
	}

	//count Ironic nodes
	ironicClient, err := newIronicClient(provider, eo)
	if err != nil {
		return nil, "", err
	}
	nodes, err := ironicClient.GetNodes()
	if err != nil {
		return nil, "", err
	}

	//Ironic bPods are expected to be listed as compute hosts assigned to
	//host aggregates in the format: "nova-compute-ironic-xxxx".
	azs, _, err := getAggregates(novaClient)
	if err != nil {
		return nil, "", err
	}
	azForHostStub := make(map[string]string)
	for azName, az := range azs {
		for host := range az.ContainsComputeHost {
			if host == "nova-compute-ironic" {
				azForHostStub["bpod001"] = azName //hardcoded for the few nodes using legacy naming convention
			} else {
				match := computeHostStubRx.FindStringSubmatch(host)
				if match == nil {
					logg.Error(`compute host %q does not match the "nova-compute-(ironic-)xxxx" naming convention`, host)
				} else {
					azForHostStub[match[1]] = azName
				}
			}
		}
	}

	unmatchedCounter := uint64(0)
	for _, node := range nodes {
		//do not consider nodes that have not been made available for provisioning yet
		if !isAvailableProvisionState[node.StableProvisionState()] {
			continue
		}

		matched := false
		for _, flavor := range flavors {
			if node.Matches(flavor) {
				logg.Debug("Ironic node %q (%s) matches flavor %s", node.Name, node.ID, flavor.Name)

				data := result[p.ftt.LimesResourceNameForFlavor(flavor.Name)]
				data.Capacity++

				var nodeAZ string
				if match := nodeNameRx.FindStringSubmatch(node.Name); match != nil {
					nodeAZ = azForHostStub[match[1]]
					if nodeAZ == "" {
						logg.Info("Ironic node %q (%s) does not match any compute host from host aggregates", node.Name, node.ID)
						nodeAZ = "unknown"
					}
				} else {
					logg.Error(`Ironic node %q (%s) does not match the "nodeXXX-{bm,bb}YYY" naming convention`, node.Name, node.ID)
				}

				if _, ok := data.CapacityPerAZ[nodeAZ]; !ok {
					data.CapacityPerAZ[nodeAZ] = &core.CapacityDataForAZ{}
				}
				data.CapacityPerAZ[nodeAZ].Capacity++
				if node.StableProvisionState() == "active" {
					data.CapacityPerAZ[nodeAZ].Usage++
				}

				if p.reportSubcapacities {
					sub := map[string]interface{}{
						"id":   node.ID,
						"name": node.Name,
					}
					if node.Properties.MemoryMiB > 0 {
						sub["ram"] = limes.ValueWithUnit{Unit: limes.UnitMebibytes, Value: uint64(node.Properties.MemoryMiB)}
					}
					if node.Properties.DiskGiB > 0 {
						sub["disk"] = limes.ValueWithUnit{Unit: limes.UnitGibibytes, Value: uint64(node.Properties.DiskGiB)}
					}
					if node.Properties.Cores > 0 {
						sub["cores"] = uint64(node.Properties.Cores)
					}
					if node.Properties.SerialNumber != "" {
						sub["serial"] = node.Properties.SerialNumber
					}
					if node.InstanceID != nil && *node.InstanceID != "" {
						sub["instance_id"] = *node.InstanceID
					}
					data.Subcapacities = append(data.Subcapacities, sub)
				}

				matched = true
				break
			}
		}
		if !matched {
			logg.Error("Ironic node %q (%s) does not match any baremetal flavor", node.Name, node.ID)
			unmatchedCounter++
		}
	}

	//remove pointers from `result`
	result2 := make(map[string]core.CapacityData, len(result))
	for resourceName, data := range result {
		result2[resourceName] = *data
	}

	serializedMetrics, _ := json.Marshal(capacitySapccIronicSerializedMetrics{
		UnmatchedNodeCount: unmatchedCounter,
	})
	return map[string]map[string]core.CapacityData{"compute": result2}, string(serializedMetrics), nil
}

var ironicUnmatchedNodesGauge = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "limes_unmatched_ironic_nodes",
		Help: "Number of available/active Ironic nodes without matching flavor.",
	},
	[]string{"os_cluster"},
)

//DescribeMetrics implements the core.CapacityPlugin interface.
func (p *capacitySapccIronicPlugin) DescribeMetrics(ch chan<- *prometheus.Desc) {
	ironicUnmatchedNodesGauge.Describe(ch)
}

//CollectMetrics implements the core.CapacityPlugin interface.
func (p *capacitySapccIronicPlugin) CollectMetrics(ch chan<- prometheus.Metric, clusterID, serializedMetrics string) error {
	if serializedMetrics == "" {
		return nil
	}
	var metrics capacitySapccIronicSerializedMetrics
	err := json.Unmarshal([]byte(serializedMetrics), &metrics)
	if err != nil {
		return err
	}

	descCh := make(chan *prometheus.Desc, 1)
	ironicUnmatchedNodesGauge.Describe(descCh)
	ironicUnmatchedNodesDesc := <-descCh

	ch <- prometheus.MustNewConstMetric(
		ironicUnmatchedNodesDesc,
		prometheus.GaugeValue, float64(metrics.UnmatchedNodeCount),
		clusterID,
	)
	return nil
}

func (p *capacitySapccIronicPlugin) collectIronicFlavorInfo(novaClient *gophercloud.ServiceClient) ([]ironicFlavorInfo, error) {
	//which flavors have separate instance quota?
	flavorNames, err := p.ftt.ListFlavorsWithSeparateInstanceQuota(novaClient)
	if err != nil {
		return nil, err
	}
	isRelevantFlavorName := make(map[string]bool, len(flavorNames))
	for _, flavorName := range flavorNames {
		isRelevantFlavorName[flavorName] = true
	}

	//collect basic attributes for flavors
	var result []ironicFlavorInfo
	err = flavorsmodule.ListDetail(novaClient, nil).EachPage(func(page pagination.Page) (bool, error) {
		flavors, err := flavorsmodule.ExtractFlavors(page)
		if err != nil {
			return false, err
		}

		for _, flavor := range flavors {
			if isRelevantFlavorName[flavor.Name] {
				result = append(result, ironicFlavorInfo{
					ID:           flavor.ID,
					Name:         flavor.Name,
					Cores:        uint64(flavor.VCPUs),
					MemoryMiB:    uint64(flavor.RAM),
					DiskGiB:      uint64(flavor.Disk),
					Capabilities: make(map[string]string),
				})
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	//retrieve extra specs - the ones in the "capabilities" namespace are
	//relevant for Ironic node selection
	for _, flavor := range result {
		extraSpecs, err := getFlavorExtras(novaClient, flavor.ID)
		if err != nil {
			return nil, err
		}
		for key, value := range extraSpecs {
			if strings.HasPrefix(key, "capabilities:") {
				capability := strings.TrimPrefix(key, "capabilities:")
				flavor.Capabilities[capability] = value
			}
		}
	}
	return result, nil
}

func (n ironicNode) Matches(f ironicFlavorInfo) bool {
	if uint64(n.Properties.Cores) != f.Cores {
		logg.Debug("core mismatch: %d != %d", n.Properties.Cores, f.Cores)
		return false
	}
	if uint64(n.Properties.MemoryMiB) != f.MemoryMiB {
		logg.Debug("memory mismatch: %d != %d", n.Properties.MemoryMiB, f.MemoryMiB)
		return false
	}
	if uint64(n.Properties.DiskGiB) != f.DiskGiB {
		logg.Debug("disk mismatch: %d != %d", n.Properties.DiskGiB, f.DiskGiB)
		return false
	}

	nodeCaps := make(map[string]string)
	if n.Properties.CPUArchitecture != "" {
		nodeCaps["cpu_arch"] = n.Properties.CPUArchitecture
	}
	for _, field := range strings.Split(n.Properties.Capabilities, ",") {
		fields := strings.SplitN(field, ":", 2)
		if len(fields) == 2 {
			nodeCaps[fields[0]] = fields[1]
		}
	}

	for key, flavorValue := range f.Capabilities {
		nodeValue, exists := nodeCaps[key]
		if !exists || nodeValue != flavorValue {
			logg.Debug("capability %s mismatch: %q != %q", key, nodeValue, flavorValue)
			return false
		}
	}
	return true
}
