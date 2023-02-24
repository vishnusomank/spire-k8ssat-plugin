package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/andres-erbsen/clock"
	"github.com/hashicorp/go-hclog"
	"github.com/spiffe/spire-plugin-sdk/pluginmain"
	"github.com/spiffe/spire/pkg/common/telemetry"
	workloadattestorv1 "github.com/vishnusomank/spire-plugin-sdk/proto/spire/plugin/agent/workloadattestor/v1"
	configv1 "github.com/vishnusomank/spire-plugin-sdk/proto/spire/service/common/config/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/kubernetes"
)

const (
	pluginName               = "k8sw_sat"
	defaultMaxPollAttempts   = 60
	defaultPollRetryInterval = time.Second * 5
	defaultReloadInterval    = time.Minute
)

type Plugin struct {
	workloadattestorv1.UnsafeWorkloadAttestorServer
	configv1.UnsafeConfigServer

	log    hclog.Logger
	clock  clock.Clock
	client *kubernetes.Clientset
}

func New() *Plugin {
	return &Plugin{
		clock: clock.New(),
	}
}

func (p *Plugin) SetLogger(log hclog.Logger) {
	p.log = log
}

func (p *Plugin) Attest(ctx context.Context, req *workloadattestorv1.AttestRequest) (*workloadattestorv1.AttestResponse, error) {

	p.client = ConnectK8sClient()

	if p.client == nil {
		return &workloadattestorv1.AttestResponse{}, fmt.Errorf("could not connect to k8s")
	}

	log := p.log.With(
		telemetry.Details, req.Meta,
	)

	for attempt := 1; ; attempt++ {

		log = log.With(telemetry.Attempt, attempt)
		var selectorValues []string

		var attestResponse *workloadattestorv1.AttestResponse
		token, ok := req.Meta["sa_token"]
		if !ok {
			return &workloadattestorv1.AttestResponse{}, nil
		}

		fmt.Printf("token: %v\n", token)
		selectors, err := p.validateServiceAccountToken(token, log)
		if err != nil {
			if strings.Contains(err.Error(), "tokenReview failed") {
				return &workloadattestorv1.AttestResponse{}, nil
			}
			return &workloadattestorv1.AttestResponse{}, err
		}
		selectorValues = append(selectorValues, selectors...)

		if len(selectorValues) > 0 {
			attestResponse = &workloadattestorv1.AttestResponse{SelectorValues: selectorValues}
		}

		if attestResponse != nil {
			return attestResponse, nil
		}

		// if the container was not located after the maximum number of attempts then the search is over.
		if attempt >= defaultMaxPollAttempts {
			log.Warn("Decoding failed; giving up")
			return nil, status.Error(codes.DeadlineExceeded, "no selectors found after max poll attempts")
		}

		select {
		case <-p.clock.After(defaultPollRetryInterval):
		case <-ctx.Done():
			return nil, status.Errorf(codes.Canceled, "no selectors found: %v", ctx.Err())
		}
	}

}

func (p *Plugin) Configure(ctx context.Context, req *configv1.ConfigureRequest) (resp *configv1.ConfigureResponse, err error) {
	return &configv1.ConfigureResponse{}, nil
}

func main() {
	plugin := new(Plugin)
	// Serve the plugin. This function call will not return. If there is a
	// failure to serve, the process will exit with a non-zero exit code.
	pluginmain.Serve(
		workloadattestorv1.WorkloadAttestorPluginServer(plugin),
		// TODO: Remove if no configuration is required
		configv1.ConfigServiceServer(plugin),
	)
}
