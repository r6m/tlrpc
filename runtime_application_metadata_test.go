package tlrpc

import (
	"context"
	"testing"

	"github.com/r6m/tlrpc/session"
)

func TestRuntimeApplicationClientMetadataPresenceAfterSessionRotation(t *testing.T) {
	for _, test := range []struct {
		name    string
		client  session.ClientMetadata
		present bool
	}{
		{name: "fresh Android session without repeated initialization"},
		{name: "initialized session", client: session.ClientMetadata{APIID: 100001, DeviceModel: "Android"}, present: true},
		{name: "different application remains visible", client: session.ClientMetadata{APIID: 100002}, present: true},
		{name: "invalid zero ID with declared metadata remains visible", client: session.ClientMetadata{DeviceModel: "Android"}, present: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := runtimeApplicationRequest(t, 101, "value")
			request.Info.Client = test.client
			ctx := runtimeApplicationHandlerContext(context.Background(), request, &runtimeMutationCollector{}, EncodeLimits{})
			metadata, present := ClientMetadataFromContext(ctx)
			if metadata != test.client || present != test.present {
				t.Fatalf("client metadata = %+v, present=%t; want %+v, present=%t", metadata, present, test.client, test.present)
			}
		})
	}
}
