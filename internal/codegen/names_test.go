package codegen

import "testing"

func TestNamerMappingRules(t *testing.T) {
	n := NewNamer()

	tests := []struct {
		name     string
		input    string
		got      string
		want     string
		callFunc func(string) string
	}{
		{"constructor user", "user", n.ConstructorName("user"), "User", n.ConstructorName},
		{"constructor userEmpty", "userEmpty", n.ConstructorName("userEmpty"), "UserEmpty", n.ConstructorName},
		{"type User", "User", n.TypeName("User"), "User", n.TypeName},
		{"method auth.sendCode", "auth.sendCode", n.MethodName("auth.sendCode"), "SendCode", n.MethodName},
		{"field send_code", "send_code", n.FieldName("send_code"), "SendCode", n.FieldName},
		{"field reply_to_msg_id", "reply_to_msg_id", n.FieldName("reply_to_msg_id"), "ReplyToMsgID", n.FieldName},
		{"field user_id", "user_id", n.FieldName("user_id"), "UserID", n.FieldName},
		{"type mtproto.Object", "mtproto.Object", n.TypeName("mtproto.Object"), "mtproto.Object", n.TypeName},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Fatalf("%s: got %q want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestNamerReservedKeywords(t *testing.T) {
	n := NewNamer()

	if got := n.FieldName("type"); got != "Type_" {
		t.Fatalf("expected reserved keyword to be suffixed: got %q", got)
	}

	if got := n.FieldName("range"); got != "Range_" {
		t.Fatalf("expected reserved keyword to be suffixed: got %q", got)
	}
}

func TestNamerServiceAndPackage(t *testing.T) {
	n := NewNamer()

	if got := n.ServiceName("auth"); got != "AuthServer" {
		t.Fatalf("expected AuthServer, got %q", got)
	}

	if got := n.PackageName("auth"); got != "auth" {
		t.Fatalf("expected auth, got %q", got)
	}
}
