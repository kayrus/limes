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

package api

import (
	"fmt"
	"net/http"
	"strings"

	gorp "gopkg.in/gorp.v2"

	"github.com/sapcc/limes/pkg/collector"
	"github.com/sapcc/limes/pkg/db"
	"github.com/sapcc/limes/pkg/limes"
	"github.com/sapcc/limes/pkg/reports"
	"github.com/sapcc/limes/pkg/util"
)

//ListDomains handles GET /v1/domains.
func (p *v1Provider) ListDomains(w http.ResponseWriter, r *http.Request) {
	if !p.CheckToken(r).Require(w, "domain:list") {
		return
	}

	domains, err := reports.GetDomains(p.Cluster, nil, db.DB, reports.ReadFilter(r))
	if ReturnError(w, err) {
		return
	}

	ReturnJSON(w, 200, map[string]interface{}{"domains": domains})
}

//GetDomain handles GET /v1/domains/:domain_id.
func (p *v1Provider) GetDomain(w http.ResponseWriter, r *http.Request) {
	if !p.CheckToken(r).Require(w, "domain:show") {
		return
	}
	dbDomain := p.FindDomainFromRequest(w, r)
	if dbDomain == nil {
		return
	}

	domains, err := reports.GetDomains(p.Cluster, &dbDomain.ID, db.DB, reports.ReadFilter(r))
	if ReturnError(w, err) {
		return
	}
	if len(domains) == 0 {
		http.Error(w, "no resource data found for domain", 500)
		return
	}

	ReturnJSON(w, 200, map[string]interface{}{"domain": domains[0]})
}

//DiscoverDomains handles POST /v1/domains/discover.
func (p *v1Provider) DiscoverDomains(w http.ResponseWriter, r *http.Request) {
	if !p.CheckToken(r).Require(w, "domain:discover") {
		return
	}

	newDomainUUIDs, err := collector.ScanDomains(p.Cluster, collector.ScanDomainsOpts{})
	if ReturnError(w, err) {
		return
	}

	if len(newDomainUUIDs) == 0 {
		w.WriteHeader(204)
		return
	}
	ReturnJSON(w, 202, map[string]interface{}{"new_domains": util.IDsToJSON(newDomainUUIDs)})
}

//PutDomain handles PUT /v1/domains/:domain_id.
func (p *v1Provider) PutDomain(w http.ResponseWriter, r *http.Request) {
	token := p.CheckToken(r)
	canRaise := token.Check("domain:raise")
	canLower := token.Check("domain:lower")
	if !canRaise && !canLower {
		token.Require(w, "domain:raise") //produce standard Unauthorized response
		return
	}

	dbDomain := p.FindDomainFromRequest(w, r)
	if dbDomain == nil {
		return
	}

	//parse request body
	var parseTarget struct {
		Domain struct {
			Services ServiceQuotas `json:"services"`
		} `json:"domain"`
	}
	parseTarget.Domain.Services = make(ServiceQuotas)
	if !RequireJSON(w, r, &parseTarget) {
		return
	}
	serviceQuotas := parseTarget.Domain.Services

	//start a transaction for the quota updates
	tx, err := db.DB.Begin()
	if ReturnError(w, err) {
		return
	}
	defer db.RollbackUnlessCommitted(tx)

	//gather a report on the domain's quotas to decide whether a quota update is legal
	domainReports, err := reports.GetDomains(p.Cluster, &dbDomain.ID, db.DB, reports.Filter{})
	if ReturnError(w, err) {
		return
	}
	if len(domainReports) == 0 {
		http.Error(w, "no resource data found for domain", 500)
		return
	}
	domainReport := domainReports[0]

	//check all services for resources to update
	var services []db.DomainService
	_, err = tx.Select(&services,
		`SELECT * FROM domain_services WHERE domain_id = $1 ORDER BY type`, dbDomain.ID)
	if ReturnError(w, err) {
		return
	}
	var resourcesToUpdate []db.DomainResource
	var resourcesToUpdateAsUntyped []interface{}
	var errors []string

	var auditTrail util.AuditTrail
	for _, srv := range services {
		resourceQuotas, exists := serviceQuotas[srv.Type]
		if !exists {
			continue
		}

		//check all resources
		var resources []db.DomainResource
		_, err = tx.Select(&resources,
			`SELECT * FROM domain_resources WHERE service_id = $1 ORDER BY name`, srv.ID)
		if ReturnError(w, err) {
			return
		}
		for _, res := range resources {
			newQuota, exists := resourceQuotas[res.Name]
			if !exists {
				continue
			}
			if res.Quota == newQuota {
				continue //nothing to do
			}

			err := checkDomainQuotaUpdate(srv, res, domainReport, newQuota, canRaise, canLower)
			if err != nil {
				errors = append(errors, err.Error())
				continue
			}

			//take a copy of the loop variable (it will be updated by the loop, so if
			//we didn't take a copy manually, the resourcesToUpdateAsUntyped list
			//would contain only identical pointers)
			res := res
			auditTrail.Add("set quota %s.%s = %d -> %d for domain %s by user %s (%s)",
				srv.Type, res.Name, res.Quota, newQuota,
				dbDomain.UUID, token.UserUUID, token.UserName,
			)
			res.Quota = newQuota
			resourcesToUpdate = append(resourcesToUpdate, res)
			resourcesToUpdateAsUntyped = append(resourcesToUpdateAsUntyped, &res)
		}
	}

	//if not legal, report errors to the user
	if len(errors) > 0 {
		http.Error(w, strings.Join(errors, "\n"), 422)
		return
	}

	//update the DB with the new quotas
	onlyQuota := func(c *gorp.ColumnMap) bool {
		return c.ColumnName == "quota"
	}
	_, err = tx.UpdateColumns(onlyQuota, resourcesToUpdateAsUntyped...)
	if ReturnError(w, err) {
		return
	}
	err = tx.Commit()
	if ReturnError(w, err) {
		return
	}
	auditTrail.Commit()

	//otherwise, report success
	domains, err := reports.GetDomains(p.Cluster, &dbDomain.ID, db.DB, reports.ReadFilter(r))
	if ReturnError(w, err) {
		return
	}
	if len(domains) == 0 {
		http.Error(w, "no resource data found for domain", 500)
		return
	}

	ReturnJSON(w, 200, map[string]interface{}{"domain": domains[0]})
}

func checkDomainQuotaUpdate(srv db.DomainService, res db.DomainResource, domain *reports.Domain, newQuota uint64, canRaise, canLower bool) error {
	//if quota is being raised, only permission is required (overprovisioning of
	//domain quota over the cluster capacity is explicitly allowed because
	//capacity measurements are usually to be taken with a grain of salt)
	if res.Quota < newQuota {
		if canRaise {
			return nil
		}
		return fmt.Errorf("cannot change %s/%s quota: user is not allowed to raise quotas in this project", srv.Type, res.Name)
	}

	//if quota is being lowered, permission is required and the domain quota may
	//not be less than the sum of quotas that the domain gives out to projects
	if !canLower {
		return fmt.Errorf("cannot change %s/%s quota: user is not allowed to lower quotas in this project", srv.Type, res.Name)
	}
	projectsQuota := uint64(0)
	var unit limes.Unit
	if domainService, exists := domain.Services[srv.Type]; exists {
		if domainResource, exists := domainService.Resources[res.Name]; exists {
			projectsQuota = domainResource.ProjectsQuota
			unit = domainResource.Unit
		}
	}
	if newQuota < projectsQuota {
		return fmt.Errorf(
			"cannot change %s/%s quota: domain quota may not be smaller than sum of project quotas in that domain (%s)",
			srv.Type, res.Name,
			unit.Format(projectsQuota),
		)
	}

	return nil
}
