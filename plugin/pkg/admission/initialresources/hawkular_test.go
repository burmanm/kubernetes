/*
Copyright 2015 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package initialresources

import (
	"fmt"
	"github.com/hawkular/hawkular-client-go/metrics"
	"k8s.io/kubernetes/pkg/api"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	assert "github.com/stretchr/testify/require"
)

func TestTaqQuery(t *testing.T) {
	image := "hawkular/hawkular-metrics:latest"
	kind := api.ResourceCPU
	tQ := tagQuery(kind, image, false)

	assert.Equal(t, 2, len(tQ))
	assert.Equal(t, tQ[containerImageTag], "hawkular/hawkular-metrics:*")
	assert.Equal(t, tQ[descriptorTag], "cpu/usage")

	tQe := tagQuery(kind, image, true)
	assert.Equal(t, 2, len(tQe))
	assert.Equal(t, tQe[containerImageTag], "hawkular/hawkular-metrics:latest")
	assert.Equal(t, tQ[descriptorTag], "cpu/usage")
}

func TestGetUsagePercentile(t *testing.T) {
	image := "hawkular/hawkular-metrics:latest"
	reqs := make(map[string]string)

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant := r.Header.Get("Hawkular-Tenant")
		if strings.Contains(r.RequestURI, "metrics/metrics?tags=") {
			reqs["tagQuery"] = r.RequestURI
			fmt.Fprintf(w, `[{ "id": "test.ir.1", "tenantId": "%s", "type": "counter", "tags": { "%s": "cpu/usage", "%s": "%s" } },
{ "id": "test.ir.2", "tenantId": "%s", "type": "counter", "tags": { "%s": "cpu/usage", "%s": "%s" } }]`, tenant, descriptorTag, containerImageTag, image, tenant, descriptorTag, containerImageTag, image)
		} else if strings.Contains(r.RequestURI, "counters/test.ir.1") {
			// fmt.Fprintf(w, `[`)
			// fmt.Fprintf(w, `{
			fmt.Printf("%d\n", metrics.UnixMilli(time.Now()))
			reqs["test.ir.1"] = r.RequestURI
		} else if strings.Contains(r.RequestURI, "counters/test.ir.2") {
			reqs["test.ir.2"] = r.RequestURI
			fmt.Fprintf(w, `{}`)
		} else {
			reqs["unknown"] = r.RequestURI
		}
	}))

	paramUri := fmt.Sprintf("%s?useNamespace=false", s.URL)

	hSource, err := newHawkularSource(paramUri)
	assert.NoError(t, err)

	_, _, err = hSource.GetUsagePercentile(api.ResourceCPU, 90, "hawkular/hawkular-metrics:latest", true, time.Now(), time.Now())
	assert.NoError(t, err)

	assert.Equal(t, 3, len(reqs))
	assert.Equal(t, "", reqs["unknown"])
}
