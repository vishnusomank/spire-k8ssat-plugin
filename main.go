package main

import (
	"github.com/spiffe/spire-plugin-sdk/pluginmain"
	workloadattestorv1 "github.com/spiffe/spire-plugin-sdk/proto/spire/plugin/agent/workloadattestor/v1"
	configv1 "github.com/spiffe/spire-plugin-sdk/proto/spire/service/common/config/v1"
	sat "github.com/vishnusomank/spire-k8ssat-plugin/cmd/k8ssat"
)

func main() {
	p := sat.New()
	pluginmain.Serve(
		workloadattestorv1.WorkloadAttestorPluginServer(p),
		// TODO: Remove if no configuration is required
		configv1.ConfigServiceServer(p),
	)
}
