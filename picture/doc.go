// Package picture checks the pictures users upload for a service to store and show, such as a
// profile picture or a project logo. The browser crops and encodes each picture, and the service
// stores it only once [Check] accepts it under the [Rule] of its contract.
//
// Check reads the whole picture and bounds no byte size, so a service caps the request body first.
// Importing the package registers the JPEG, PNG and WebP decoders with the image package.
package picture
