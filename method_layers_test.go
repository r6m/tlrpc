package tlrpc

import "testing"

func TestMethodLayerRanges(t *testing.T) {
	old := MethodDesc{MinLayer: 228, MaxLayer: 228}
	current := MethodDesc{MinLayer: 229, MaxLayer: 229}
	if methodLayersOverlap(old, current) {
		t.Fatal("disjoint layers overlap")
	}
	if !methodLayersOverlap(old, MethodDesc{}) {
		t.Fatal("historical wildcard must overlap")
	}
	if !methodLayersOverlap(current, MethodDesc{MinLayer: 229}) {
		t.Fatal("inclusive lower bound")
	}
	if !methodSupportsLayer(old, 228) || methodSupportsLayer(old, 229) || !methodSupportsLayer(MethodDesc{}, 229) {
		t.Fatal("wrong layer selection")
	}
}

func TestRegisterServiceRejectsOverlappingWireVariants(t *testing.T) {
	for _, ranges := range [][4]int{{228, 229, 229, 230}, {0, 0, 229, 229}, {229, 228, 0, 0}, {-1, 228, 229, 229}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("accepted invalid ranges %v", ranges)
				}
			}()
			server := NewServer()
			method := MethodDesc{MethodName: "Call", ConstructorID: runtimeApplicationRequestID, MinLayer: ranges[0], MaxLayer: ranges[1], NewRequest: func() TLObject { return &runtimeApplicationTestRequest{} }, Handler: runtimeApplicationMethodHandler, EncodeResponse: encodeRuntimeApplicationTestResponse}
			other := method
			other.MethodName = "CallOther"
			other.MinLayer = ranges[2]
			other.MaxLayer = ranges[3]
			server.RegisterService(ServiceDesc{ServiceName: "layered", SchemaLayer: 229, HandlerType: (*runtimeApplicationTestService)(nil), Methods: []MethodDesc{method, other}}, &runtimeApplicationTestServiceImpl{})
		}()
	}
}
