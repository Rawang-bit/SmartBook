package main

import (
	"log"
	"net/http"
	"time"

	"bookroom-management-system/controllers"
	"bookroom-management-system/database"
	"bookroom-management-system/models"
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
	sessions   := session.New(db)
	controller := controllers.New(db, sessions)

	go runBookingRetentionJob(controller.Bookings)

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

// runBookingRetentionJob permanently purges booking records older than
// models.BookingRetentionDays. It runs once at startup and then once a day
// for as long as the process is alive.
func runBookingRetentionJob(bookings *models.BookingModel) {
	purge := func() {
		n, err := bookings.PurgeOldBookings()
		if err != nil {
			log.Printf("[RETENTION] failed to purge bookings older than %d days: %v", models.BookingRetentionDays, err)
			return
		}
		if n > 0 {
			log.Printf("[RETENTION] purged %d booking(s) older than %d days", n, models.BookingRetentionDays)
		}
	}

	purge()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		purge()
	}
}
