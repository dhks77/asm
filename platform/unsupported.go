package platform

func newUnsupportedPlatform() Platform {
	return &platformImpl{
		name:           "unsupported",
		notify:         func(string, string) {},
		prepareIDEOpen: appendPathWithName,
	}
}
