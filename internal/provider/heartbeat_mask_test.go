package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/sutramx/terraform-provider-sutramx/internal/client"
)

// A read_only API key gets the heartbeat URL masked; refreshing with one
// must not overwrite the known URL in state with the mask.
func TestApplyMonitorKeepsHeartbeatURLWhenMasked(t *testing.T) {
	masked := maskedSecret
	real := "https://api.sutramx.com/heartbeat/abc"
	monitor := client.Monitor{ID: "m1", Name: "job", Type: "cron", IsActive: true, Config: []byte(`{}`), HeartbeatURL: &masked}

	var diags diag.Diagnostics
	model := monitorModel{HeartbeatURL: types.StringValue(real), ConfigJSON: types.StringNull()}
	applyMonitor(context.Background(), monitor, &model, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if got := model.HeartbeatURL.ValueString(); got != real {
		t.Fatalf("heartbeat_url = %q, want the prior %q", got, real)
	}

	// No prior value (import with a read-only key): the mask is all there is.
	fresh := monitorModel{HeartbeatURL: types.StringNull(), ConfigJSON: types.StringNull()}
	applyMonitor(context.Background(), monitor, &fresh, &diags)
	if got := fresh.HeartbeatURL.ValueString(); got != masked {
		t.Fatalf("heartbeat_url = %q, want %q", got, masked)
	}

	// A real URL from the API always wins.
	monitor.HeartbeatURL = &real
	other := monitorModel{HeartbeatURL: types.StringValue("https://old.example/x"), ConfigJSON: types.StringNull()}
	applyMonitor(context.Background(), monitor, &other, &diags)
	if got := other.HeartbeatURL.ValueString(); got != real {
		t.Fatalf("heartbeat_url = %q, want %q", got, real)
	}
}
