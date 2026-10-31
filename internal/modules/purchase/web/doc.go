// Package purchaseweb holds purchase's screens and module routes (PO spec
// §11, §20.7, §21 to §25). It registers plain handlers; internal/app puts
// AUTH's guard on each route (CD-46). Each route calls a handler in its
// owner's stubs_web_<task>.go until that task builds it (CD-256).
package purchaseweb
