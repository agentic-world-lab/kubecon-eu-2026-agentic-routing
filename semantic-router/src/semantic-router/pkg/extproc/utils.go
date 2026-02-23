package extproc

import (
	ext_proc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
)

// sendResponse sends a response back to Envoy/Agent Gateway
func sendResponse(stream ext_proc.ExternalProcessor_ProcessServer, response *ext_proc.ProcessingResponse, msgType string) error {
	if response == nil {
		logging.Warnf("[RESPONSE-SEND] Nil response for %s, skipping", msgType)
		return nil
	}

	logging.Infof("[RESPONSE-SEND] Sending %s response", msgType)

	if err := stream.Send(response); err != nil {
		logging.Errorf("[RESPONSE-SEND] Failed to send %s response: %v", msgType, err)
		return err
	}

	return nil
}
