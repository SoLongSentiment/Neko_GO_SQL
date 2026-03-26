package nekosql

type Migration struct {
	Version int
	Name    string
	SQL     string
}

func DemoMigrations() []Migration {
	return []Migration{
		{
			Version: 1,
			Name:    "players",
			SQL:     "CREATE TABLE players (id INT PRIMARY KEY, name TEXT, mmr INT)",
		},
		{
			Version: 2,
			Name:    "matches",
			SQL:     "CREATE TABLE matches (id INT PRIMARY KEY, winner TEXT, turns INT)",
		},
	}
}
