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

package util

import (
	"fmt"
	"time"
)

//Time is like time.Time, but can be scanned from a SQLite query where the
//result is an int64 (a UNIX timestamp).
type Time time.Time

//Scan implements the sql.Scanner interface.
func (t *Time) Scan(src interface{}) error {
	switch val := src.(type) {
	case int64:
		*t = Time(time.Unix(val, 0))
		return nil
	case time.Time:
		*t = Time(val)
		return nil
	default:
		return fmt.Errorf("cannot scan %t into util.Time", val)
	}
}