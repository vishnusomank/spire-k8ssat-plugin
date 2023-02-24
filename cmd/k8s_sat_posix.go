package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang-jwt/jwt"
	"github.com/rs/zerolog/log"
	review "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var parsed bool = false
var kubeconfig *string

func isInCluster() bool {
	if _, ok := os.LookupEnv("KUBERNETES_PORT"); ok {
		return true
	}

	return false
}

func ConnectK8sClient() *kubernetes.Clientset {
	if isInCluster() {
		return ConnectInClusterAPIClient()
	}

	return ConnectLocalAPIClient()
}

func ConnectLocalAPIClient() *kubernetes.Clientset {
	if !parsed {
		homeDir := ""
		if h := os.Getenv("HOME"); h != "" {
			homeDir = h
		} else {
			homeDir = os.Getenv("USERPROFILE") // windows
		}

		envKubeConfig := os.Getenv("KUBECONFIG")
		if envKubeConfig != "" {
			kubeconfig = &envKubeConfig
		} else {
			if home := homeDir; home != "" {
				kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
			} else {
				kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
			}
			flag.Parse()
		}

		parsed = true
	}

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Error().Msg(err.Error())
		return nil
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Error().Msg(err.Error())
		return nil
	}

	return clientset
}

func ConnectInClusterAPIClient() *kubernetes.Clientset {
	host := ""
	port := ""
	token := ""

	if val, ok := os.LookupEnv("KUBERNETES_SERVICE_HOST"); ok {
		host = val
	} else {
		host = "127.0.0.1"
	}

	if val, ok := os.LookupEnv("KUBERNETES_PORT_443_TCP_PORT"); ok {
		port = val
	} else {
		port = "6443"
	}

	read, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		log.Error().Msg(err.Error())
		return nil
	}

	token = string(read)

	// create the configuration by token
	kubeConfig := &rest.Config{
		Host:        "https://" + host + ":" + port,
		BearerToken: token,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}

	if client, err := kubernetes.NewForConfig(kubeConfig); err != nil {
		log.Error().Msg(err.Error())
		return nil
	} else {
		return client
	}
}

var defaultSAToken = "eyJhbGciOiJSUzI1NiIsImtpZCI6ImRUcVFNWVQ0YlM4b0dORnh0QVpmbER6Y3QwRTVGWWdUeWlDRkpkUURKTUUifQ.eyJhdWQiOlsiaHR0cHM6Ly9rdWJlcm5ldGVzLmRlZmF1bHQuc3ZjLmNsdXN0ZXIubG9jYWwiLCJrM3MiXSwiZXhwIjoxNzA4MTA2MTQ4LCJpYXQiOjE2NzY1NzAxNDgsImlzcyI6Imh0dHBzOi8va3ViZXJuZXRlcy5kZWZhdWx0LnN2Yy5jbHVzdGVyLmxvY2FsIiwia3ViZXJuZXRlcy5pbyI6eyJuYW1lc3BhY2UiOiJkZWZhdWx0IiwicG9kIjp7Im5hbWUiOiJzaCIsInVpZCI6IjRhZjNmMWJhLTZmOTMtNGZlMS1iMzA0LWFhNDBiOWRlODNmZCJ9LCJzZXJ2aWNlYWNjb3VudCI6eyJuYW1lIjoiZGVmYXVsdCIsInVpZCI6IjQ1MGQyZWRmLTVmYTYtNGY1Yi05MDJhLTVlNzUyZWY4ZGM2YiJ9LCJ3YXJuYWZ0ZXIiOjE2NzY1NzM3NTV9LCJuYmYiOjE2NzY1NzAxNDgsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDpkZWZhdWx0OmRlZmF1bHQifQ.Eu9Gl2mWRbJ_RUDha-sSkI3s9SYPHYxUF1H8B2qrkbuUksYdM5uTKfpdtUfAr4CN6E_SY6NbOrZUln_XabgIsweG0qOH2yEdOXITVBX_0gidjvt2eRvQOXu-PnQVaYT_XODO2ggdAwUoIoAUvXdWjTBaYdPkVjqZe0_VvWi2jRh1KyJcivyOGdF2S_SZ9s5vcbH40fydW-MsbdhZjLR-VckeSX_YJGJkgtHnOjTMDkOA7mwJ0Yz17I-xQQ-hsgCLd7VB8-hXC4RmJQtOeqZInDXNMOmQVljhdm6Nmu4vLfMtPkWZpue9PnT2vcjO9FhLUwlpZEcCaDN3tQG88q2ypA"

func (p *Plugin) getSelectorValuesFromPodName(podName, podNs string) []string {

	if podName == "" {
		return nil
	}
	var selectorValues []string

	pod, err := p.client.CoreV1().Pods(podNs).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		log.Error().Msg(err.Error())
		return nil
	}

	if pod.Name == podName {
		selectorValues = []string{
			fmt.Sprintf("sa:%s", pod.Spec.ServiceAccountName),
			fmt.Sprintf("ns:%s", pod.Namespace),
			fmt.Sprintf("pod-uid:%s", pod.UID),
			fmt.Sprintf("pod-name:%s", pod.Name),
		}
		for k, v := range pod.Labels {
			selectorValues = append(selectorValues, fmt.Sprintf("pod-label:%s:%s", k, v))
		}
	}

	return selectorValues
}

func (p *Plugin) validateServiceAccountToken(token string) []string {
	var podName string

	result, err := p.client.AuthenticationV1().TokenReviews().Create(context.Background(), &review.TokenReview{
		Spec: review.TokenReviewSpec{
			Token: token,
		},
	}, metav1.CreateOptions{})

	if err != nil || !result.Status.Authenticated {
		return nil
	}

	for key, value := range result.Status.User.Extra {
		if strings.Contains(key, "pod-name") {
			podName = value[0]
			break
		}
	}

	jwtToken, _, err := new(jwt.Parser).ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		return nil
	}
	kubernetesMap := jwtToken.Claims.(jwt.MapClaims)["kubernetes.io"].(map[string]interface{})

	podNs := kubernetesMap["namespace"].(string)

	return p.getSelectorValuesFromPodName(podName, podNs)
}
