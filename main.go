package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"log"
	"net/http"
)

type NginxConfig struct {
	Namespace string `json:"namespace"`
	Ingress   string `json:"ingress"`
	Config    string `json:"config"`
}

func sendNginxConfig(config NginxConfig) error {
	url := "http://localhost:3033/api/v1/nginx/conf"
	payload, err := json.Marshal(config)
	if err != nil {
		return err
	}
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		log.Printf("Error sending HTTP POST request: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Received non-OK status code: %d", resp.StatusCode)
	} else {
		log.Printf("Successfully sent nginx config for ingress %s in namespace %s", config.Ingress, config.Namespace)
	}

	return nil
}

func main() {
	const PLUGIN_HUB_SERVICE_NAME = "satpam-service-app"
	const PLUGIN_HUB_PORT = 8080
	const SIDRA_PLUGINS_KEY = "sidra.id/plugins"

	// Load kubernetes config from default location (~/.kube/config)
	config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		log.Fatalf("Error loading kubeconfig: %v", err)
	}

	// Create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating kubernetes client: %v", err)
	}

	// Get list of all namespaces
	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Fatalf("Error getting namespaces: %v", err)
	}

	// Iterate through each namespace and get ingresses
	for _, ns := range namespaces.Items {
		ingresses, err := clientset.NetworkingV1().Ingresses(ns.Name).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			log.Printf("Error getting ingresses in namespace %s: %v", ns.Name, err)
			continue
		}

		// Generate nginx config for ingresses in this namespace
		if len(ingresses.Items) > 0 {
			fmt.Printf("\n# Ingresses in namespace %s:\n", ns.Name)
			for _, ing := range ingresses.Items {
				conf := NginxConfig{
					Namespace: ns.Name,
					Ingress:   ing.Name,
					Config:    "",
				}
				conf.Config += "server {\n"
				conf.Config += "   listen 8080;\n"
				conf.Config += fmt.Sprintf("  server_name %s;\n", ing.Spec.Rules[0].Host)

				// Generate location blocks for each rule
				for _, rule := range ing.Spec.Rules {
					if rule.HTTP != nil {
						for _, path := range rule.HTTP.Paths {
							conf.Config += fmt.Sprintf("  location %s {\n", path.Path)
							conf.Config += fmt.Sprintf("    proxy_pass http://%s:%v;\n",
								PLUGIN_HUB_SERVICE_NAME,
								PLUGIN_HUB_PORT,
							)
							conf.Config += fmt.Sprintf("    proxy_set_header ServiceName %s;\n", path.Backend.Service.Name)
							conf.Config += fmt.Sprintf("    proxy_set_header ServicePort %d;\n", path.Backend.Service.Port.Number)
							conf.Config += fmt.Sprintf("    proxy_set_header Host %s;\n", rule.Host)
							conf.Config += fmt.Sprintf("    proxy_set_header Plugins %s;\n", ing.GetAnnotations()[SIDRA_PLUGINS_KEY])
							conf.Config += "  }\n"
						}
					}
				}
				conf.Config += "}\n"

				err := sendNginxConfig(conf)
				if err != nil {
					log.Printf("Error sending nginx config for ingress %s in namespace %s: %v", ing.Name, ns.Name, err)
					continue
				}
			}
		}
	}
}
