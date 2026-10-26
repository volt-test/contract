package definition

// Internal values the external tests check against the schema and README.
var (
	ExtractTypesForTest     = extractTypes
	DataFormatsForTest      = dataFormats
	DataModesForTest        = dataModes
	CheckTypesForTest       = checkTypes
	FailModesForTest        = failModes
	JSONKindsForTest        = jsonKinds
	HTTPVersionsForTest     = httpVersions
	TLSVersionsForTest      = tlsVersions
	MinEngineReleaseForTest = minEngineRelease
)

// MaxKeptForTest is how many problems or paths an error keeps, and
// MaxShownForTest how many its message lists.
const (
	MaxKeptForTest  = maxKept
	MaxShownForTest = maxShown
)
