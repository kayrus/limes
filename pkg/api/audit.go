/*******************************************************************************
*
* Copyright 2019 SAP SE
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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/gopherpolicy"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/hermes/pkg/cadf"
	"github.com/sapcc/limes"
	"github.com/sapcc/limes/pkg/core"
	"github.com/streadway/amqp"
)

var showAuditOnStdout = os.Getenv("LIMES_SILENT") != "1"

func init() {
	log.SetOutput(os.Stdout)
	if os.Getenv("LIMES_DEBUG") == "1" {
		logg.ShowDebug = true
	}
}

//eventSinkPerCluster is a map of cluster ID to a channel that receives audit events.
var eventSinkPerCluster map[string]chan<- cadf.Event

//StartAuditTrail starts the audit trail by initializing the EventSinkPerCluster
//and starting separate Commit() goroutines per Cluster.
func StartAuditTrail(configPerCluster map[string]core.CADFConfiguration) {
	eventSinkPerCluster = make(map[string]chan<- cadf.Event)
	for clusterID, config := range configPerCluster {
		if config.Enabled {
			labels := prometheus.Labels{
				"os_cluster": clusterID,
			}
			auditEventPublishSuccessCounter.With(labels).Add(0)
			auditEventPublishFailedCounter.With(labels).Add(0)

			onSuccessFunc := func() {
				auditEventPublishSuccessCounter.With(labels).Inc()
			}
			onFailFunc := func() {
				auditEventPublishFailedCounter.With(labels).Inc()
			}
			s := make(chan cadf.Event, 20)
			eventSinkPerCluster[clusterID] = s

			if config.RabbitMQ.Username == "" {
				config.RabbitMQ.Username = "guest"
			}
			if config.RabbitMQ.Password == "" {
				config.RabbitMQ.Password = "guest"
			}
			if config.RabbitMQ.Hostname == "" {
				config.RabbitMQ.Hostname = "localhost"
			}
			if config.RabbitMQ.Port == 0 {
				config.RabbitMQ.Port = 5672
			}
			rabbitURI := amqp.URI{
				Scheme:   "amqp",
				Host:     config.RabbitMQ.Hostname,
				Port:     config.RabbitMQ.Port,
				Username: config.RabbitMQ.Username,
				Password: string(config.RabbitMQ.Password),
				Vhost:    "/",
			}

			go audittools.AuditTrail{
				EventSink:           s,
				OnSuccessfulPublish: onSuccessFunc,
				OnFailedPublish:     onFailFunc,
			}.Commit(config.RabbitMQ.QueueName, rabbitURI)
		}
	}
}

var observerUUID = audittools.GenerateUUID()

//logAndPublishEvent takes the necessary parameters and generates a cadf.Event.
//It logs the event to stdout and publishes it to a RabbitMQ server.
func logAndPublishEvent(clusterID string, time time.Time, req *http.Request, token *gopherpolicy.Token, reasonCode int, target audittools.TargetRenderer) {
	p := audittools.EventParameters{
		Time:       time,
		Request:    req,
		User:       token,
		ReasonCode: reasonCode,
		Action:     "update",
		Observer: struct {
			TypeURI string
			Name    string
			ID      string
		}{
			TypeURI: "service/resources",
			Name:    "limes",
			ID:      observerUUID,
		},
		Target: target,
	}
	event := audittools.NewEvent(p)

	if showAuditOnStdout {
		msg, _ := json.Marshal(event)
		logg.Other("AUDIT", string(msg))
	}

	s := eventSinkPerCluster[clusterID]
	if s != nil {
		s <- event
	}
}

//quotaEventTarget contains the structure for rendering a cadf.Event.Target for
//changes regarding resource quota.
type quotaEventTarget struct {
	DomainID     string
	ProjectID    string
	ServiceType  string
	ResourceName string
	OldQuota     uint64
	NewQuota     uint64
	QuotaUnit    limes.Unit
	RejectReason string
}

//Render implements the audittools.TargetRenderer interface type.
func (t quotaEventTarget) Render() cadf.Resource {
	targetID := t.ProjectID
	if t.ProjectID == "" {
		targetID = t.DomainID
	}

	return cadf.Resource{
		TypeURI:   fmt.Sprintf("service/%s/%s/quota", t.ServiceType, t.ResourceName),
		ID:        targetID,
		DomainID:  t.DomainID,
		ProjectID: t.ProjectID,
		Attachments: []cadf.Attachment{{
			Name:    "payload",
			TypeURI: "mime:application/json",
			Content: targetAttachmentContent{
				OldQuota:     t.OldQuota,
				NewQuota:     t.NewQuota,
				Unit:         t.QuotaUnit,
				RejectReason: t.RejectReason},
		}},
	}
}

//burstEventTarget contains the structure for rendering a cadf.Event.Target for
//changes regarding quota bursting for some project.
type burstEventTarget struct {
	DomainID     string
	ProjectID    string
	NewStatus    bool
	RejectReason string
}

//Render implements the audittools.TargetRenderer interface type.
func (t burstEventTarget) Render() cadf.Resource {
	return cadf.Resource{
		TypeURI:   "service/resources/bursting",
		ID:        t.ProjectID,
		DomainID:  t.DomainID,
		ProjectID: t.ProjectID,
		Attachments: []cadf.Attachment{{
			Name:    "payload",
			TypeURI: "mime:application/json",
			Content: targetAttachmentContent{
				NewStatus:    t.NewStatus,
				RejectReason: t.RejectReason},
		}},
	}
}

//rateLimitEventTarget contains the structure for rendering a cadf.Event.Target for
//changes regarding rate limits
type rateLimitEventTarget struct {
	DomainID     string
	ProjectID    string
	ServiceType  string
	Name         string
	Unit         limes.Unit
	OldLimit     uint64
	NewLimit     uint64
	OldWindow    limes.Window
	NewWindow    limes.Window
	RejectReason string
}

//Render implements the audittools.TargetRenderer interface type.
func (t rateLimitEventTarget) Render() cadf.Resource {
	return cadf.Resource{
		TypeURI:   fmt.Sprintf("service/%s/%s/rates", t.ServiceType, t.Name),
		ID:        t.ProjectID,
		DomainID:  t.DomainID,
		ProjectID: t.ProjectID,
		Attachments: []cadf.Attachment{
			{
				Name:    "payload",
				TypeURI: "mime:application/json",
				Content: targetAttachmentContent{
					Unit:         t.Unit,
					OldLimit:     t.OldLimit,
					NewLimit:     t.NewLimit,
					OldWindow:    t.OldWindow,
					NewWindow:    t.NewWindow,
					RejectReason: t.RejectReason,
				},
			},
		},
	}
}

//This type is needed for the custom MarshalJSON behavior.
type targetAttachmentContent struct {
	RejectReason string
	// for quota or rate limit changes
	Unit limes.Unit
	// for quota changes
	OldQuota uint64
	NewQuota uint64
	// for quota bursting
	NewStatus bool
	// for rate limit changes
	OldLimit  uint64
	NewLimit  uint64
	OldWindow limes.Window
	NewWindow limes.Window
}

//MarshalJSON implements the json.Marshaler interface.
func (a targetAttachmentContent) MarshalJSON() ([]byte, error) {
	//copy data into a struct that does not have a custom MarshalJSON
	data := struct {
		OldQuota     uint64       `json:"oldQuota,omitempty"`
		NewQuota     uint64       `json:"newQuota,omitempty"`
		Unit         limes.Unit   `json:"unit,omitempty"`
		NewStatus    bool         `json:"newStatus,omitempty"`
		RejectReason string       `json:"rejectReason,omitempty"`
		OldLimit     uint64       `json:"oldLimit,omitempty"`
		NewLimit     uint64       `json:"newLimit,omitempty"`
		OldWindow    limes.Window `json:"oldWindow,omitempty"`
		NewWindow    limes.Window `json:"newWindow,omitempty"`
	}{
		OldQuota:     a.OldQuota,
		NewQuota:     a.NewQuota,
		NewStatus:    a.NewStatus,
		Unit:         a.Unit,
		RejectReason: a.RejectReason,
		OldLimit:     a.OldLimit,
		NewLimit:     a.NewLimit,
		OldWindow:    a.OldWindow,
		NewWindow:    a.NewWindow,
	}
	//Hermes does not accept a JSON object at target.attachments[].content, so
	//we need to wrap the marshaled JSON into a JSON string
	bytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(bytes))
}
