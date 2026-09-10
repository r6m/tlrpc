package tlrpc

func methodSupportsLayer(method MethodDesc, layer int) bool {
	return (method.MinLayer == 0 || layer >= method.MinLayer) && (method.MaxLayer == 0 || layer <= method.MaxLayer)
}

func methodLayersOverlap(a, b MethodDesc) bool {
	return (a.MaxLayer == 0 || b.MinLayer <= a.MaxLayer) && (b.MaxLayer == 0 || a.MinLayer <= b.MaxLayer)
}
