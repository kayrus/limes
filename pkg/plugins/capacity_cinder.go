/*******************************************************************************
*
* Copyright 2017 SAP SE
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
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/limes"
	"github.com/sapcc/limes/pkg/core"
	"github.com/sapcc/limes/pkg/util"
)

type capacityCinderPlugin struct {
	cfg core.CapacitorConfiguration
}

func init() {
	core.RegisterCapacityPlugin(func(c core.CapacitorConfiguration, scrapeSubcapacities map[string]map[string]bool) core.CapacityPlugin {
		return &capacityCinderPlugin{c}
	})
}

//Init implements the core.CapacityPlugin interface.
func (p *capacityCinderPlugin) Init(provider *gophercloud.ProviderClient, eo gophercloud.EndpointOpts) error {
	return nil
}

//ID implements the core.CapacityPlugin interface.
func (p *capacityCinderPlugin) ID() string {
	return "cinder"
}

//Scrape implements the core.CapacityPlugin interface.
func (p *capacityCinderPlugin) Scrape(provider *gophercloud.ProviderClient, eo gophercloud.EndpointOpts, clusterID string) (map[string]map[string]core.CapacityData, error) {
	client, err := openstack.NewBlockStorageV2(provider, eo)
	if err != nil {
		return nil, err
	}

	var result gophercloud.Result

	//Get absolute limits for a tenant
	url := client.ServiceURL("scheduler-stats", "get_pools") + "?detail=True"
	_, err = client.Get(url, &result.Body, nil)
	if err != nil {
		return nil, err
	}

	var limitData struct {
		Pools []struct {
			Name         string `json:"name"`
			Capabilities struct {
				TotalCapacityGb     util.Float64OrUnknown `json:"total_capacity_gb"`
				AllocatedCapacityGb util.Float64OrUnknown `json:"allocated_capacity_gb"`
				VolumeBackendName   string                `json:"volume_backend_name"`
			} `json:"capabilities"`
		} `json:"pools"`
	}
	err = result.ExtractInto(&limitData)
	if err != nil {
		return nil, err
	}

	url = client.ServiceURL("os-services")
	_, err = client.Get(url, &result.Body, nil)
	if err != nil {
		return nil, err
	}

	var servicesData struct {
		Services []struct {
			Binary           string `json:"binary"`
			AvailabilityZone string `json:"zone"`
			Host             string `json:"host"`
		} `json:"services"`
	}
	err = result.ExtractInto(&servicesData)
	if err != nil {
		return nil, err
	}

	serviceHostsPerAZ := make(map[string][]string)
	for _, element := range servicesData.Services {
		if element.Binary == "cinder-volume" {
			//element.Host has the format backendHostname@backendName
			serviceHostsPerAZ[element.AvailabilityZone] = append(serviceHostsPerAZ[element.AvailabilityZone], element.Host)
		}
	}

	var (
		totalCapacityGb uint64
		capacityPerAZ   = make(limes.ClusterAvailabilityZoneReports)

		volumeBackendName = p.cfg.Cinder.VolumeBackendName
	)

	//add results from scheduler-stats
	for _, element := range limitData.Pools {
		if (volumeBackendName != "") && (element.Capabilities.VolumeBackendName != volumeBackendName) {
			logg.Debug("Not considering %s with volume_backend_name %s", element.Name, element.Capabilities.VolumeBackendName)
			continue
		}

		logg.Debug("Considering %s with volume_backend_name %s", element.Name, element.Capabilities.VolumeBackendName)

		totalCapacityGb += uint64(element.Capabilities.TotalCapacityGb)

		var poolAZ string
		for az, hosts := range serviceHostsPerAZ {
			for _, v := range hosts {
				//element.Name has the format backendHostname@backendName#backendPoolName
				if strings.Contains(element.Name, v) {
					poolAZ = az
					break
				}
			}
		}
		if poolAZ == "" {
			logg.Info("Cinder storage pool %q does not match any service host", element.Name)
			poolAZ = "unknown"
		}
		if _, ok := capacityPerAZ[poolAZ]; !ok {
			capacityPerAZ[poolAZ] = &limes.ClusterAvailabilityZoneReport{Name: poolAZ}
		}

		capacityPerAZ[poolAZ].Capacity += uint64(element.Capabilities.TotalCapacityGb)
		capacityPerAZ[poolAZ].Usage += uint64(element.Capabilities.AllocatedCapacityGb)
	}

	return map[string]map[string]core.CapacityData{
		"volumev2": {
			"capacity": core.CapacityData{Capacity: totalCapacityGb, CapacityPerAZ: capacityPerAZ},
			//NOTE: no estimates for no. of snapshots/volumes here; this depends highly on the backend
			//(on SAP CC, we configure capacity for snapshots/volumes via the "manual" capacitor)
		},
	}, nil
}
