// Package notify implements the MOCA notification system.
//
// Notifications can be delivered via email (SMTP or SES), browser push,
// in-app notification feed, or SMS. Delivery channels are configured per
// site and per user preference.
//
// Key components:
//   - Email: SMTP and AWS SES adapters for transactional email
//   - Push: browser push notification via Web Push protocol
//   - InApp: in-application notification feed with read/unread tracking
//   - SMS: SMS delivery via configurable provider adapters
package notify
