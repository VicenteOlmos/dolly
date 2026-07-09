package brand

const Name = "dolly"

// SheepFace is the default cowsay sheep eyes segment (UooU).
const SheepFace = "UooU"

// SheepMark is an alias for SheepFace.
const SheepMark = SheepFace

func Header() string {
	return SheepFace + " " + Name
}
