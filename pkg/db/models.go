/*******************************************************************************
*
* Copyright 2017-2020 SAP SE
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

package db

import (
	"time"

	"github.com/sapcc/limes"
)

//ClusterCapacitor contains a record from the `cluster_capacitors` table.
type ClusterCapacitor struct {
	ClusterID          string     `db:"cluster_id"`
	CapacitorID        string     `db:"capacitor_id"`
	ScrapedAt          *time.Time `db:"scraped_at"` //pointer type to allow for NULL value
	ScrapeDurationSecs float64    `db:"scrape_duration_secs"`
	SerializedMetrics  string     `db:"serialized_metrics"`
}

//ClusterService contains a record from the `cluster_services` table.
type ClusterService struct {
	ID        int64      `db:"id"`
	ClusterID string     `db:"cluster_id"`
	Type      string     `db:"type"`
	ScrapedAt *time.Time `db:"scraped_at"` //pointer type to allow for NULL value
}

//ClusterResource contains a record from the `cluster_resources` table.
type ClusterResource struct {
	ServiceID         int64  `db:"service_id"`
	Name              string `db:"name"`
	RawCapacity       uint64 `db:"capacity"`
	CapacityPerAZJSON string `db:"capacity_per_az"`
	SubcapacitiesJSON string `db:"subcapacities"`
}

//Domain contains a record from the `domains` table.
type Domain struct {
	ID        int64  `db:"id"`
	ClusterID string `db:"cluster_id"`
	Name      string `db:"name"`
	UUID      string `db:"uuid"`
}

//DomainService contains a record from the `domain_services` table.
type DomainService struct {
	ID       int64  `db:"id"`
	DomainID int64  `db:"domain_id"`
	Type     string `db:"type"`
}

//DomainResource contains a record from the `domain_resources` table.
type DomainResource struct {
	ServiceID int64  `db:"service_id"`
	Name      string `db:"name"`
	Quota     uint64 `db:"quota"`
}

//Project contains a record from the `projects` table.
type Project struct {
	ID          int64  `db:"id"`
	DomainID    int64  `db:"domain_id"`
	Name        string `db:"name"`
	UUID        string `db:"uuid"`
	ParentUUID  string `db:"parent_uuid"`
	HasBursting bool   `db:"has_bursting"`
}

//ProjectService contains a record from the `project_services` table.
type ProjectService struct {
	ID                      int64      `db:"id"`
	ProjectID               int64      `db:"project_id"`
	Type                    string     `db:"type"`
	ScrapedAt               *time.Time `db:"scraped_at"` //pointer type to allow for NULL value
	Stale                   bool       `db:"stale"`
	ScrapeDurationSecs      float64    `db:"scrape_duration_secs"`
	RatesScrapedAt          *time.Time `db:"rates_scraped_at"` //same as above
	RatesStale              bool       `db:"rates_stale"`
	RatesScrapeDurationSecs float64    `db:"rates_scrape_duration_secs"`
	RatesScrapeState        string     `db:"rates_scrape_state"`
	SerializedMetrics       string     `db:"serialized_metrics"`
}

//ProjectResource contains a record from the `project_resources` table. Quota
//values are NULL for resources that do not track quota.
type ProjectResource struct {
	ServiceID           int64   `db:"service_id"`
	Name                string  `db:"name"`
	Quota               *uint64 `db:"quota"`
	Usage               uint64  `db:"usage"`
	PhysicalUsage       *uint64 `db:"physical_usage"`
	BackendQuota        *int64  `db:"backend_quota"`
	DesiredBackendQuota *uint64 `db:"desired_backend_quota"`
	SubresourcesJSON    string  `db:"subresources"`
}

//ProjectRate contains a record from the `project_rates` table.
type ProjectRate struct {
	ServiceID     int64         `db:"service_id"`
	Name          string        `db:"name"`
	Limit         *uint64       `db:"rate_limit"`      // nil for rates that don't have a limit (just a usage)
	Window        *limes.Window `db:"window_ns"`       // nil for rates that don't have a limit (just a usage)
	UsageAsBigint string        `db:"usage_as_bigint"` // empty for rates that don't have a usage (just a limit)
	//^ NOTE: Postgres has a NUMERIC type that would be large enough to hold an
	//  uint128, but Go does not have a uint128 builtin, so it's easier to just
	//  use strings throughout and cast into bigints in the scraper only.
}

//InitGorp is used by Init() to setup the ORM part of the database connection.
//It's available as an exported function because the unit tests need to call
//this while bypassing the normal Init() logic.
func InitGorp() {
	DB.AddTableWithName(ClusterCapacitor{}, "cluster_capacitors").SetKeys(false, "cluster_id", "capacitor_id")
	DB.AddTableWithName(ClusterService{}, "cluster_services").SetKeys(true, "id")
	DB.AddTableWithName(ClusterResource{}, "cluster_resources").SetKeys(false, "service_id", "name")
	DB.AddTableWithName(Domain{}, "domains").SetKeys(true, "id")
	DB.AddTableWithName(DomainService{}, "domain_services").SetKeys(true, "id")
	DB.AddTableWithName(DomainResource{}, "domain_resources").SetKeys(false, "service_id", "name")
	DB.AddTableWithName(Project{}, "projects").SetKeys(true, "id")
	DB.AddTableWithName(ProjectService{}, "project_services").SetKeys(true, "id")
	DB.AddTableWithName(ProjectResource{}, "project_resources").SetKeys(false, "service_id", "name")
	DB.AddTableWithName(ProjectRate{}, "project_rates").SetKeys(false, "service_id", "name")
}
