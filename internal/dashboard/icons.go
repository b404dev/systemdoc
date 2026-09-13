package dashboard

// iconRole is a semantic vocabulary, not a collection of decorative aliases.
// Nerd Font glyphs are paired with a readable Unicode/ASCII fallback so labels
// keep their meaning in an unpatched terminal or a remote session.
type iconRole int

const (
	iconEye iconRole = iota
	iconServices
	iconContainers
	iconNetwork
	iconProcesses
	iconStorage
	iconDeck
	iconViews
	iconActions
	iconThemes
	iconExpand
	iconFilter
	iconOverview
	iconLogs
	iconConfig
	iconResources
	iconDependencies
	iconRefresh
	iconHealthy
	iconAttention
	iconIdle
	iconFavorite
	iconChanged
	iconSearch
	iconApprove
	iconCancel
	iconLock
	iconStoryline
	iconConstellation
)

type iconPair struct{ nerd, fallback string }

var iconVocabulary = map[iconRole]iconPair{
	iconEye:           {"\uf06e", "◉"},
	iconServices:      {"\uf233", "SVC"},
	iconContainers:    {"\uf21f", "CTR"},
	iconNetwork:       {"\uf0e8", "NET"},
	iconProcesses:     {"\uf2db", "PROC"},
	iconStorage:       {"\uf0a0", "DISK"},
	iconDeck:          {"\uf06e", "DECK"},
	iconViews:         {"\uf02e", "VIEWS"},
	iconActions:       {"\uf0e7", "ACT"},
	iconThemes:        {"\uf1fc", "THEME"},
	iconExpand:        {"\uf065", "EXPAND"},
	iconFilter:        {"\uf0b0", "FILTER"},
	iconOverview:      {"\uf06e", "VIEW"},
	iconLogs:          {"\uf1ea", "LOG"},
	iconConfig:        {"\uf013", "CFG"},
	iconResources:     {"\uf201", "METRIC"},
	iconDependencies:  {"\uf1b2", "DEPS"},
	iconRefresh:       {"\uf021", "REFRESH"},
	iconHealthy:       {"\uf058", "●"},
	iconAttention:     {"\uf071", "!"},
	iconIdle:          {"\uf10c", "○"},
	iconFavorite:      {"\uf005", "★"},
	iconChanged:       {"\uf0e7", "›"},
	iconSearch:        {"\uf002", "/"},
	iconApprove:       {"\uf00c", "YES"},
	iconCancel:        {"\uf00d", "NO"},
	iconLock:          {"\uf023", "!"},
	iconStoryline:     {"\uf1da", "TIME"},
	iconConstellation: {"\uf1b3", "MAP"},
}

func iconFor(nerd bool, role iconRole) string {
	pair := iconVocabulary[role]
	if nerd {
		return pair.nerd
	}
	return pair.fallback
}

func (w *workspace) icon(role iconRole) string { return iconFor(w.settings.NerdIcons, role) }

func (w *workspace) iconLabel(role iconRole, label string) string {
	return w.icon(role) + " " + label
}
