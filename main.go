// Copyright 2016 Andrew Stuart

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"text/template"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/watch"
	"k8s.io/client-go/rest"
)

const (
	k8sNeedle      = "\n\n##BEGIN K8S HOSTS##\n"
	zoneTmpl       = k8sNeedle + "{{ range $ip, $names := .ips }}{{ $ip }}\t{{ range $hostname := $names }} {{$hostname}}{{end}}\n{{end}}"
	defaultMaxErrs = 10
)

var (
	inCluster = flag.Bool("incluster", false, "the client is running inside a kuberenetes cluster")
	once      = flag.Bool("once", false, "Write the file and then exit; do not watch for ingress changes")
	filepath  = flag.String("filepath", "/etc/hosts", "File location for zone file")
	kubeHost  = flag.String("host", "", "The kubernetes v1 host; required if not run in-cluster")
	maxErrs   = flag.Int("max-errs", defaultMaxErrs, "The number of errors acceptable before quitting")

	spaceRE = regexp.MustCompile("[[:space:]]+")
	ztpl    = template.Must(template.New("bind9").Parse(zoneTmpl))
)

func init() {
	flag.Parse()

	if *maxErrs < 0 {
		*maxErrs = defaultMaxErrs
	}
}

func main() {
	var config *rest.Config

	// sigs := make(chan os.Signal)
	// go signal.Notify(sigs, os.Kill, os.Interrupt)
	// go func() {
	// 	for sig := range sigs {

	// 	}
	// }()

	if *inCluster {
		var err error
		config, err = rest.InClusterConfig()
		if err != nil {
			log.Fatal("Error getting in-cluster config: ", err)
		}
	} else {
		if *kubeHost == "" {
			flag.Usage()
			log.Fatal("Must run with -incluster (inside a k8s cluster) or provide a kubernetes host via -host")
		}
		config = &rest.Config{
			Host: *kubeHost,
		}
	}

	cli, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal("Error creating v1 client: ", err)
	}

	defer cleanup()

	if !*once {
		log.Fatal(watchIng(cli))
	}

	numErrs := 0
	for {
		if err := createBindFile(cli); err != nil {
			numErrs++
			log.Println("Bind file creation error ", err)
			if numErrs > *maxErrs {
				os.Exit(1)
			}
		}
	}
}

func cleanup() {
	orig, err := getOrig()
	if err != nil {
		log.Fatal("Error during cleanup while reading original file", err)
	}

	f, err := os.OpenFile(*filepath, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0640)
	if err != nil {
		log.Fatal("Error during cleanup while opening file for write", err)
	}
	defer f.Close()

	_, err = f.Write(orig)
	if err != nil {
		log.Fatal("Error during cleanup while writing original file state", err)
	}
}

func createBindFile(c *kubernetes.Clientset) error {
	ingresses := map[string][]string{}

	ings, err := c.Ingresses("").List(v1.ListOptions{})
	if err != nil {
		return err
	}

	for _, ing := range ings.Items {
		log.Println(ing.Name)

		if len(ing.Status.LoadBalancer.Ingress) < 1 {
			log.Println("No ingresses to load for ", ing)
			continue
		}

		ip := ing.Status.LoadBalancer.Ingress[0].IP

		if ingresses[ip] == nil {
			ingresses[ip] = []string{}
		}

		for _, rule := range ing.Spec.Rules {
			host := rule.Host

			if host == "" {
				log.Printf("Not adding empty host entry for ingress %s (was %s)\n", ing.Name, rule.Host)
				continue
			}

			a := append(ingresses[ip], host)
			ingresses[ip] = a
		}
	}
	log.Println()

	prev, err := getOrig()
	if err != nil {
		return err
	}

	f, err := os.OpenFile(*filepath, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0640)
	if err != nil {
		return err
	}

	f.Write(prev)

	err = ztpl.Execute(f, map[string]interface{}{"ips": ingresses})
	if err != nil {
		return err
	}

	return f.Close()
}

func getOrig() ([]byte, error) {
	bs, err := ioutil.ReadFile(*filepath)
	if err != nil {
		return nil, err
	}

	arr := bytes.Split(bs, []byte(k8sNeedle))

	if arr == nil || len(arr) < 1 {
		return nil, fmt.Errorf("No result from splitting bytes")
	}

	return arr[0], nil
}

func watchIng(cli *kubernetes.Clientset) error {
	for {
		w, err := cli.Ingresses("").Watch(v1.ListOptions{})
		if err != nil {
			return fmt.Errorf("Watch error %s", err)
		}

		for evt := range w.ResultChan() {
			et := watch.EventType(evt.Type)
			if et != watch.Added && et != watch.Modified {
				continue
			}

			err = createBindFile(cli)
			if err != nil {
				return err
			}
		}

		log.Println("Result channel closed. Starting again.")
	}
}
