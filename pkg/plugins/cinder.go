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
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/sapcc/limes/pkg/limes"
)

type cinderPlugin struct{}

var cinderResources = []limes.ResourceInfo{
	limes.ResourceInfo{
		Name: "capacity",
		Unit: limes.UnitGibibytes,
	},
	limes.ResourceInfo{
		Name: "snapshots",
		Unit: limes.UnitNone,
	},
	limes.ResourceInfo{
		Name: "volumes",
		Unit: limes.UnitNone,
	},
}

func init() {
	limes.RegisterQuotaPlugin(&cinderPlugin{})
}

//ServiceType implements the limes.QuotaPlugin interface.
func (p *cinderPlugin) ServiceType() string {
	return "volumev2"
}

//Resources implements the limes.QuotaPlugin interface.
func (p *cinderPlugin) Resources() []limes.ResourceInfo {
	return cinderResources
}

func (p *cinderPlugin) Client(driver limes.Driver) (*gophercloud.ServiceClient, error) {
	return openstack.NewBlockStorageV2(driver.Client(),
		gophercloud.EndpointOpts{Availability: gophercloud.AvailabilityPublic},
	)
}

//Scrape implements the limes.QuotaPlugin interface.
func (p *cinderPlugin) Scrape(driver limes.Driver, domainUUID, projectUUID string) (map[string]limes.ResourceData, error) {
	client, err := p.Client(driver)
	if err != nil {
		return nil, err
	}

	var result gophercloud.Result
	url := client.ServiceURL("os-quota-sets", projectUUID) + "?usage=True"
	_, err = client.Get(url, &result.Body, nil)
	if err != nil {
		return nil, err
	}

	type field struct {
		Quota int64  `json:"limit"`
		Usage uint64 `json:"in_use"`
	}
	var data struct {
		QuotaSet struct {
			Capacity  field `json:"gigabytes"`
			Snapshots field `json:"snapshots"`
			Volumes   field `json:"volumes"`
		} `json:"quota_set"`
	}
	err = result.ExtractInto(&data)
	if err != nil {
		return nil, err
	}

	return map[string]limes.ResourceData{
		"capacity": limes.ResourceData{
			Quota: data.QuotaSet.Capacity.Quota,
			Usage: data.QuotaSet.Capacity.Usage,
		},
		"snapshots": limes.ResourceData{
			Quota: data.QuotaSet.Snapshots.Quota,
			Usage: data.QuotaSet.Snapshots.Usage,
		},
		"volumes": limes.ResourceData{
			Quota: data.QuotaSet.Volumes.Quota,
			Usage: data.QuotaSet.Volumes.Usage,
		},
	}, nil
}

//SetQuota implements the limes.QuotaPlugin interface.
func (p *cinderPlugin) SetQuota(driver limes.Driver, domainUUID, projectUUID string, quotas map[string]uint64) error {
	requestData := map[string]map[string]uint64{
		"quota_set": map[string]uint64{
			"gigabytes": quotas["gigabytes"],
			"snapshots": quotas["snapshots"],
			"volumes":   quotas["volumes"],
		},
	}

	client, err := p.Client(driver)
	if err != nil {
		return err
	}

	url := client.ServiceURL("os-quota-sets", projectUUID)
	_, err = client.Put(url, requestData, nil, &gophercloud.RequestOpts{OkCodes: []int{200}})
	return err
}