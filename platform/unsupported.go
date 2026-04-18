package platform

func newUnsupportedPlatform() Platform {
	return &platformImpl{
		name:           "unsupported",
		prepareIDEOpen: appendPathWithName,
	}
}
