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
	"crypto/tls"
	"fmt"
	"github.com/golang/glog"
	"github.com/hawkular/hawkular-client-go/metrics"
	"io/ioutil"
	"k8s.io/kubernetes/pkg/api"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	kube_client "k8s.io/kubernetes/pkg/client/unversioned"
	kubeClientCmd "k8s.io/kubernetes/pkg/client/unversioned/clientcmd"
)

type hawkularSource struct {
	client       *metrics.Client
	uri          *url.URL
	useNamespace bool
}

const (
	containerImageTag string = "container_base_image"
	descriptorTag     string = "descriptor_name"
	separator         string = "/"

	defaultServiceAccountFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

// Get equivalent MetricDescriptor.Name used in the Heapster
func heapsterEqName(kind api.ResourceName) string {
	if kind == api.ResourceCPU {
		return "cpu/usage"
	} else if kind == api.ResourceMemory {
		return "memory/usage"
	}
	return ""
}

// Create tagFilter query to get correct metricIds
func tagQuery(kind api.ResourceName, image string, exactMatch bool) map[string]string {
	q := make(map[string]string)

	// Add here the descriptor_tag..
	q[descriptorTag] = heapsterEqName(kind)

	if exactMatch {
		q[containerImageTag] = image
	} else {
		q[containerImageTag] = fmt.Sprintf("%s:*", strings.Split(image, ":")[0])
	}

	return q
}

func calculateResults(ds []*metrics.Datapoint, perc int64) (int64, int64, error) {

	ss := make([]float64, 0)

	for _, d := range ds {
		f, _ := metrics.ConvertToFloat64(d.Value)
		ss = append(ss, f)
	}

	// Doing it exactly like the GCM endpoint for now
	count := len(ss)
	sort.Float64s(ss)
	usageIndex := int64(math.Ceil(float64(count)*float64(perc)/100)) - 1
	usage := ss[usageIndex]

	return int64(usage), int64(count), nil
}

// dataSource API

func (self *hawkularSource) GetUsagePercentile(kind api.ResourceName, perc int64, image, namespace string, exactMatch bool, start, end time.Time) (int64, int64, error) {
	q := tagQuery(kind, image, exactMatch)

	mds, err := self.client.Definitions(metrics.Filters(metrics.TagsFilter(q)))
	if err != nil {
		return 0, 0, err
	}

	dses := make([]*metrics.Datapoint, 0)

	// For each metricId, search for values
	for _, md := range mds {
		ds, err := self.client.ReadMetric(md.Type, md.Id, metrics.Filters(metrics.StartTimeFilter(start), metrics.EndTimeFilter(end)))
		if err != nil {
			// TODO Should we really quit or just .. ignore this result?
			return 0, 0, err
		}
		dses = append(dses, ds...)
	}

	return calculateResults(dses, perc)
}

// Create new Hawkular Source. The uri follows the scheme from Heapster
func newHawkularSource(uri string) (dataSource, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	d := &hawkularSource{
		uri: u,
	}
	if err = d.init(); err != nil {
		return nil, err
	}
	return d, nil
}

// Almost equal to the Heapster initialization, try to keep them in the sync
func (self *hawkularSource) init() error {
	p := metrics.Parameters{
		Tenant: "heapster", // This data is stored by the heapster
		Url:    self.uri.String(),
	}

	opts := self.uri.Query()

	if v, found := opts["tenant"]; found {
		p.Tenant = v[0]
	}

	// Not necessary anymore?
	if v, found := opts["useNamespace"]; found {
		if b, _ := strconv.ParseBool(v[0]); b {
			self.useNamespace = b
		}
	}

	if v, found := opts["useServiceAccount"]; found {
		if b, _ := strconv.ParseBool(v[0]); b {
			// If a readable service account token exists, then use it
			if contents, err := ioutil.ReadFile(defaultServiceAccountFile); err == nil {
				p.Token = string(contents)
			}
		}
	}

	// Authentication / Authorization parameters
	tC := &tls.Config{}

	if v, found := opts["auth"]; found {
		if len(v[0]) > 0 {
			// Authfile
			kubeConfig, err := kubeClientCmd.NewNonInteractiveDeferredLoadingClientConfig(&kubeClientCmd.ClientConfigLoadingRules{
				ExplicitPath: v[0]},
				&kubeClientCmd.ConfigOverrides{}).ClientConfig()
			if err != nil {
				return err
			}
			tC, err = kube_client.TLSConfigFor(kubeConfig)
			if err != nil {
				return err
			}
		}
	}

	if v, found := opts["insecure"]; found {
		insecure, err := strconv.ParseBool(v[0])
		if err != nil {
			return err
		}
		tC.InsecureSkipVerify = insecure
	}

	p.TLSConfig = tC

	c, err := metrics.NewHawkularClient(p)
	if err != nil {
		return err
	}

	self.client = c

	glog.Infof("Initialised Hawkular Source with parameters %v", p)
	return nil
}
