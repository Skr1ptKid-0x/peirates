package internal

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/trung/jwt-tools/display"
	"gopkg.in/square/go-jose.v2/jwt"
)

// SERVICE ACCOUNT MANAGEMENT functions

// ServiceAccount stores service account information.
type ServiceAccount struct {
	Name            string    // Service account name
	Token           string    // Service account token
	DiscoveryTime   time.Time // Time the service account was discovered
	DiscoveryMethod string    // How the service account was discovered (file on disk, secrets, user input, etc.)
}

// ClientCertificateKeyPair stores certificate and key information for one principal.
type ClientCertificateKeyPair struct {
	Name string // Client cert-key pair name
	// ClientKeyPath         string // Client key file path
	// ClientCertificatePath string // Client cert file path
	ClientKeyData         string // Client key data
	ClientCertificateData string // Client cert data
	APIServer             string // URL like https://10.96.0.1:443
	CACert                string // Content of a CA cert
}

// AddNewServiceAccount adds a new service account to the existing slice, but only if the the new one is unique
// Return whether one was added - if it wasn't, it's a duplicate.
func AddNewServiceAccount(name, token, discoveryMethod string, serviceAccountList *[]ServiceAccount) bool {

	// Confirm we don't have this service account already.
	for _, sa := range *serviceAccountList {
		if strings.TrimSpace(sa.Name) == strings.TrimSpace(name) {
			if Verbose {
				println("DEBUG: found a service account token we already had: " + sa.Name)
			}
			return false
		}
	}

	*serviceAccountList = append(*serviceAccountList,
		ServiceAccount{
			Name:            name,
			Token:           token,
			DiscoveryTime:   time.Now(),
			DiscoveryMethod: discoveryMethod,
		})

	return true
}

func MakeClientCertificateKeyPair(name, clientCertificateData, clientKeyData, APIServer, CACert string) ClientCertificateKeyPair {
	return ClientCertificateKeyPair{
		Name:                  name,
		ClientKeyData:         clientKeyData,
		ClientCertificateData: clientCertificateData,
		APIServer:             APIServer,
		CACert:                CACert,
	}
}

func acceptServiceAccountFromUser() (ServiceAccount, error) {
	var err error
	println("\nPlease paste in a new service account token or hit ENTER to maintain current token.")
	serviceAccount := ServiceAccount{
		Name:            "",
		Token:           "",
		DiscoveryTime:   time.Now(),
		DiscoveryMethod: "User Input",
	}
	println("\nPaste the service account token and hit ENTER:")
	serviceAccount.Token, err = ReadLineStripWhitespace()
	if err != nil {
		println("Problem with white space: %v", err)
		return serviceAccount, err
	}
	if serviceAccount.Token == "" {
		return serviceAccount, errors.New("No token provided")
	}

	println("\nWhat do you want to name this service account?")
	serviceAccount.Name, err = ReadLineStripWhitespace()
	if err != nil {
		println("Problem with reading in name: %v", err)
		serviceAccount.Name = "Unnamed"
	}

	return serviceAccount, nil
}

func assignServiceAccountToConnection(account ServiceAccount, info *ServerInfo) {
	info.TokenName = account.Name
	info.Token = account.Token

	if Verbose {
		println("DEBUG: Setting token to %s", info.Token)
	}

	// Zero out any client certificate + key, so it's clear what to authenticate with.
	info.ClientCertData = ""
	info.ClientKeyData = ""
	info.ClientCertName = ""

}

func assignAuthenticationCertificateAndKeyToConnection(keypair ClientCertificateKeyPair, info *ServerInfo) {

	// Write out the CACert to a path
	const tmpFileFormat = "*-ca.crt"

	file, err := os.CreateTemp("/tmp", tmpFileFormat)
	if err != nil {
		log.Fatal(err)
	}
	CAPath := file.Name()

	if err != nil {
		println("ERROR: could not open for writing: " + CAPath)
		return
	}
	defer file.Close()

	_, err2 := file.WriteString(keypair.CACert)
	if err2 != nil {
		println("ERROR: could not write certificate authority cert to " + CAPath)
		return
	}

	info.CAPath = CAPath
	info.ClientCertData = keypair.ClientCertificateData
	info.ClientKeyData = keypair.ClientKeyData
	info.ClientCertName = keypair.Name
	info.APIServer = keypair.APIServer
	if Verbose {
		println("DEBUG: Switching API server to: " + info.APIServer)
	}
	info.Namespace = "default"

	// Zero out any service account token, so it's clear what to authenticate with.
	info.TokenName = ""
	info.Token = ""

}

func listServiceAccounts(serviceAccounts []ServiceAccount, connectionString ServerInfo, logToFile bool, outputFileName string) {
	println("\nAvailable Service Accounts:")
	// Build a string of the service accounts, with the current one marked.
	var output string
	for i, account := range serviceAccounts {
		if account.Name == connectionString.TokenName {
			output += fmt.Sprintf("> [%d] %s\n", i, account.Name)
		} else {
			output += fmt.Sprintf("  [%d] %s\n", i, account.Name)
		}
	}
	outputToUser(output, logToFile, outputFileName)
}

func switchServiceAccounts(serviceAccounts []ServiceAccount, connectionString *ServerInfo, logToFile bool, outputFileName string) {
	var err error
	listServiceAccounts(serviceAccounts, *connectionString, logToFile, outputFileName)
	println("\nEnter service account number or exit to abort: ")
	var tokNum int
	var input string
	_, err = fmt.Scanln(&input)
	if input == "exit" {
		return
	}

	_, err = fmt.Sscan(input, &tokNum)
	if err != nil {
		fmt.Printf("Error parsing service account selection: %s\n", err.Error())
	} else if tokNum < 0 || tokNum >= len(serviceAccounts) {
		fmt.Printf("Service account %d does not exist!\n", tokNum)
	} else {
		assignServiceAccountToConnection(serviceAccounts[tokNum], connectionString)
		fmt.Printf("Selected %s // %s\n", connectionString.TokenName, connectionString.Token)
	}
	return
}

func displayServiceAccountTokenInteractive(serviceAccounts []ServiceAccount, connectionString *ServerInfo, logToFile bool, outputFileName string) {
	var err error
	listServiceAccounts(serviceAccounts, *connectionString, false, outputFileName)

	println("\nEnter service account number or exit to abort: ")
	var tokNum int
	var input string
	_, err = fmt.Scanln(&input)
	if input == "exit" {
		return
	}

	_, err = fmt.Sscan(input, &tokNum)
	if err != nil {
		fmt.Printf("Error parsing service account selection: %s\n", err.Error())
	} else if tokNum < 0 || tokNum >= len(serviceAccounts) {
		fmt.Printf("Service account %d does not exist!\n", tokNum)
	} else {
		fmt.Printf("Service account %s is accessed with token %s\n", serviceAccounts[tokNum].Name, serviceAccounts[tokNum].Token)
	}
	return
}

func printJWT(tokenString string) {
	var err error
	var claims map[string]interface{}

	token, err := jwt.ParseSigned(tokenString)
	err = token.UnsafeClaimsWithoutVerification(&claims)
	if err != nil {
		println("Problem with token thingy: %v", err)
	}

	err = display.PrintJSON(claims)
}

func parseServiceAccountJWT_return_sub(tokenString string) (int64, string, error) {

	// Split the JWT into its three components
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		errorMsg := fmt.Sprintf("Invalid token: %s", tokenString)
		println(errorMsg)
		return 0, "", errors.New(errorMsg)
	}

	// Decode the payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		errorMsg := fmt.Sprintf("Error decoding payload: %v", err)
		println(errorMsg)
		return 0, "", errors.New(errorMsg)
	}

	// Parse the JSON payload
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		errorMsg := fmt.Sprintf("Error parsing JSON: %v", err)
		println(errorMsg)
		return 0, "", errors.New(errorMsg)
	}

	// Extract the "sub" field
	if sub, ok := claims["sub"].(string); ok {
		fmt.Printf("%s\n", sub)
		return 0, sub, nil
	} else {
		errorMsg := "Error: 'sub' field not found or not a string"
		println(errorMsg)
		return 0, "", errors.New(errorMsg)
	}

}

// parseServiceAccountJWT() takes in a service account JWT and returns its expiration date and name.
func parseServiceAccountJWT(tokenString string) (int64, string) {

	var claims map[string]interface{}

	token, err := jwt.ParseSigned(tokenString)
	err = token.UnsafeClaimsWithoutVerification(&claims)
	if err != nil {
		println("Problem with token thingy: %v", err)
	}
	expiration := int64(claims["exp"].(float64))

	// Parse out the name of the service account.
	// Here's what a sample JWT looks like:
	// {
	//   "aud": ["https://kubernetes.default.svc.cluster.local"],
	//   "exp": 1725391365,
	//   "iat": 1693855365,
	//   "iss": "https://kubernetes.default.svc.cluster.local",
	//   "kubernetes.io": {
	//     "namespace": "default",
	//     "pod": {
	//       "name": "web",
	//       "uid": "..."
	//     },
	//     "serviceaccount": {
	//       "name": "default",
	//       "uid": "..."
	//     },
	//     "warnafter": 1693858972
	//   },
	//   "nbf": 1693855365,
	//   "sub": "system:serviceaccount:default:default"
	// }

	kubernetesIOstruct := claims["kubernetes.io"].(map[string]interface{})
	namespace := kubernetesIOstruct["namespace"].(string)

	saStruct := kubernetesIOstruct["serviceaccount"].(map[string]interface{})
	name := saStruct["name"].(string)

	return expiration, namespace + ":" + name
}

func getServiceAccountTokenFromSecret(connectionString ServerInfo, serviceAccounts *[]ServiceAccount, interactive bool) {
	println("\nPlease enter the name of the secret for which you'd like the contents: ")
	var secretName string
	_, err := fmt.Scanln(&secretName)
	if err != nil {
		println("[-] Error reading secret name: ", err)
		pauseToHitEnter(interactive)
		return
	}

	secretJSON, _, err := runKubectlSimple(connectionString, "get", "secret", secretName, "-o", "json")
	if err != nil {
		println("[-] Could not retrieve secret")
		pauseToHitEnter(interactive)
		return
	}

	var secretData map[string]interface{}
	err = json.Unmarshal(secretJSON, &secretData)
	if err != nil {
		println("[-] Error unmarshaling secret data: ", err)
		pauseToHitEnter(interactive)
		return
	}

	secretType := secretData["type"].(string)

	/* #gosec G101 - this is not a hardcoded credential */
	if secretType != "kubernetes.io/service-account-token" {
		println("[-] This secret is not a service account token.")
		pauseToHitEnter(interactive)
		return
	}

	opaqueToken := secretData["data"].(map[string]interface{})["token"].(string)
	token, err := base64.StdEncoding.DecodeString(opaqueToken)
	if err != nil {
		println("[-] ERROR: couldn't decode")
		pauseToHitEnter(interactive)
		return
	} else {
		fmt.Printf("[+] Saved %s // %s\n", secretName, token)
		AddNewServiceAccount(secretName, string(token), "Cluster Secret", serviceAccounts)
	}
}
