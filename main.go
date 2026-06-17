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
	db, err := database.Connect()
	if err != nil {
		log.Fatal("Could not connect to the database:", err)
	}
	defer db.Close()

	port       := database.GetEnv("PORT", "8080")
	sessions   := session.New()
	controller := controllers.New(db, sessions)

	mux := http.NewServeMux()
	routes.RegisterRoutes(mux, controller)
	mux.Handle("/", http.FileServer(http.Dir("view")))

	// Build the middleware chain (outermost applied first):
	//   HTTPSRedirect — redirects plain HTTP to HTTPS in production (308 Permanent)
	//   SecureHeaders — attaches defensive security headers to every response
	var handler http.Handler = mux
	handler = controllers.SecureHeaders(handler)
	handler = controllers.HTTPSRedirect(handler)

	log.Printf("SmartBook is running at http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}
