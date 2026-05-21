package main

import (
	"log"
	"net/http"

	"bookroom-management-system/controllers"
	"bookroom-management-system/database"
	"bookroom-management-system/routes"
	"bookroom-management-system/session"
)

func main() {
	// Connect to PostgreSQL — database.Connect() loads the .env file automatically
	db, err := database.Connect()
	if err != nil {
		log.Fatal("Could not connect to the database:", err)
	}
	defer db.Close()

	port      := database.GetEnv("PORT", "8080")
	sessions  := session.New()
	controller := controllers.New(db, sessions)

	mux := http.NewServeMux()
	routes.RegisterRoutes(mux, controller)
	mux.Handle("/", http.FileServer(http.Dir("web")))

	log.Printf("SmartBook is running at http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
