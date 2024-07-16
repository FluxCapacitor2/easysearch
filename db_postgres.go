package main

import "database/sql"

type PostgresDatabase struct {
	conn *sql.DB
}

func (db *PostgresDatabase) setup() error {
	panic("Unimplemented")
}

func (db *PostgresDatabase) addDocument(url string, title string, description string, content string) (*Page, error) {
	panic("Unimplemented")
}

func (db *PostgresDatabase) search(query string) ([]Result, error) {
	panic("Unimplemented")
}

func (db *PostgresDatabase) addToQueue(urls []string) error {
	panic("Unimplemented")
}

func createPostgresDatabase(fileName string) (*PostgresDatabase, error) {
	conn, err := sql.Open("sqlite3", fileName)

	if err != nil {
		return nil, err
	}

	return &PostgresDatabase{conn}, nil
}
