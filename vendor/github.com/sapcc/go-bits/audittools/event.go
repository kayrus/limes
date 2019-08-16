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

package audittools

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gofrs/uuid"
	"github.com/sapcc/go-bits/gopherpolicy"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/hermes/pkg/cadf"
)

// TargetRenderer is the interface that different event types "must" implement
// in order to render the respective cadf.Event.Target section.
type TargetRenderer interface {
	Render() cadf.Resource
}

// EventParameters contains the necessary parameters for generating a cadf.Event.
type EventParameters struct {
	Time    time.Time
	Request *http.Request
	Token   *gopherpolicy.Token
	// ReasonCode is used to determine whether the Event.Outcome was a 'success' or 'failure'.
	// It is recommended to use a constant from: https://golang.org/pkg/net/http/#pkg-constants
	ReasonCode int
	Action     string
	Observer   struct {
		TypeURI string
		Name    string
		ID      string
	}
	Target TargetRenderer
}

// NewEvent uses EventParameters to generate an audit event.
// Warning: this function uses GenerateUUID() to generate the Event.ID, if that fails
// then the concerning error will be logged and it will result in program termination.
func NewEvent(p EventParameters) cadf.Event {
	outcome := "failure"
	if p.ReasonCode >= 200 && p.ReasonCode < 300 {
		outcome = "success"
	}

	return cadf.Event{
		TypeURI:   "http://schemas.dmtf.org/cloud/audit/1.0/event",
		ID:        GenerateUUID(),
		EventTime: p.Time.Format("2006-01-02T15:04:05.999999+00:00"),
		EventType: "activity",
		Action:    p.Action,
		Outcome:   outcome,
		Reason: cadf.Reason{
			ReasonType: "HTTP",
			ReasonCode: strconv.Itoa(p.ReasonCode),
		},
		Initiator: cadf.Resource{
			TypeURI:   "service/security/account/user",
			Name:      p.Token.Context.Auth["user_name"],
			ID:        p.Token.Context.Auth["user_id"],
			Domain:    p.Token.Context.Auth["domain_name"],
			DomainID:  p.Token.Context.Auth["domain_id"],
			ProjectID: p.Token.Context.Auth["project_id"],
			Host: &cadf.Host{
				Address: tryStripPort(p.Request.RemoteAddr),
				Agent:   p.Request.Header.Get("User-Agent"),
			},
		},
		Target: p.Target.Render(),
		Observer: cadf.Resource{
			TypeURI: p.Observer.TypeURI,
			Name:    p.Observer.Name,
			ID:      p.Observer.ID,
		},
		RequestPath: p.Request.URL.String(),
	}
}

// GenerateUUID generates an UUID based on random numbers (RFC 4122).
// Failure will result in program termination.
func GenerateUUID() string {
	u, err := uuid.NewV4()
	if err != nil {
		logg.Fatal(err.Error())
	}
	return u.String()
}

func tryStripPort(hostPort string) string {
	host, _, err := net.SplitHostPort(hostPort)
	if err == nil {
		return host
	}
	return hostPort
}
