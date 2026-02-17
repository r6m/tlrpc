package generator

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/template"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
)

// ServiceGenerator generates service interfaces and registrations.
type ServiceGenerator struct {
	namer *naming.Namer
	out   io.Writer
}

// NewServiceGenerator creates a new service generator.
func NewServiceGenerator(namer *naming.Namer, out io.Writer) *ServiceGenerator {
	return &ServiceGenerator{namer: namer, out: out}
}

// ServiceTemplateData holds data for service template
type ServiceTemplateData struct {
	Name        string
	Methods     []MethodTemplateData
	StubMethods []StubMethodTemplateData
}

// MethodTemplateData holds data for interface methods
type MethodTemplateData struct {
	Name     string
	ReqType  string
	RespType string
}

// StubMethodTemplateData holds data for unimplemented stub methods
type StubMethodTemplateData struct {
	ServiceName string
	Name        string
	ReqType     string
	RespType    string
	ZeroValue   string
}

// serviceInterfaceTemplate generates a service interface
const serviceInterfaceTemplate = `type {{.Name}} interface {
{{- range .Methods}}
	{{.Name}}(ctx context.Context, req *{{.ReqType}}) ({{.RespType}}, error)
{{- end}}
}

type Unimplemented{{.Name}} struct{}

func (Unimplemented{{.Name}}) testEmbeddedByValue() {}
{{range .StubMethods}}
func (Unimplemented{{$.Name}}) {{.Name}}(context.Context, *{{.ReqType}}) ({{.RespType}}, error) {
	return {{.ZeroValue}}, ErrMethodNotImplemented
}
{{end}}
`

// GenerateService emits service interfaces and unimplemented stubs.
func (g *ServiceGenerator) GenerateService(funcs []parser.FuncDecl) error {
	services := groupByService(funcs)
	if _, err := io.WriteString(g.out, "var ErrMethodNotImplemented = errors.New(\"tlrpc: method not implemented\")\n\n"); err != nil {
		return err
	}

	serviceNames := sortedKeys(services)
	tmpl, err := template.New("service").Funcs(templateFuncMap()).Parse(serviceInterfaceTemplate)
	if err != nil {
		return err
	}

	for _, service := range serviceNames {
		name := g.namer.ServiceName(service)

		var methods []MethodTemplateData
		var stubMethods []StubMethodTemplateData

		for _, fn := range services[service] {
			if fn.IsTemplate {
				continue
			}
			method := g.namer.MethodName(fn.Name)
			reqType := g.namer.RequestName(fn.Name)
			respType := g.responseType(fn.ResultType)

			methods = append(methods, MethodTemplateData{
				Name:     method,
				ReqType:  reqType,
				RespType: respType,
			})

			stubMethods = append(stubMethods, StubMethodTemplateData{
				ServiceName: name,
				Name:        method,
				ReqType:     reqType,
				RespType:    respType,
				ZeroValue:   zeroValue(respType),
			})
		}

		data := ServiceTemplateData{
			Name:        name,
			Methods:     methods,
			StubMethods: stubMethods,
		}

		if err := tmpl.Execute(g.out, data); err != nil {
			return err
		}
	}

	return nil
}

// GenerateRegistration emits static service descriptors and registration helpers (gRPC-like pattern).
func (g *ServiceGenerator) GenerateRegistration(funcs []parser.FuncDecl) error {
	services := groupByService(funcs)

	serviceNames := sortedKeys(services)
	for _, service := range serviceNames {
		if err := g.generateServiceDescriptor(service, services[service]); err != nil {
			return err
		}
		if err := g.generateRegistrationFunction(service, services[service]); err != nil {
			return err
		}
	}

	return nil
}

// generateServiceDescriptor emits a static ServiceDesc variable (gRPC-style).
func (g *ServiceGenerator) generateServiceDescriptor(service string, funcs []parser.FuncDecl) error {
	name := g.namer.ServiceName(service)
	descName := name + "_ServiceDesc"

	if _, err := fmt.Fprintf(g.out, "// %s is the static service descriptor for %s (gRPC-like).\nvar %s = tlrpc.ServiceDesc{\n\tServiceName: %q,\n\tHandlerType: (*%s)(nil),\n\tMethods: []tlrpc.MethodDesc{\n", descName, name, descName, service, name); err != nil {
		return err
	}

	for _, fn := range funcs {
		if fn.IsTemplate {
			continue
		}
		method := g.namer.MethodName(fn.Name)
		handlerName := fmt.Sprintf("_%s_%s_Handler", name, method)

		if _, err := fmt.Fprintf(g.out, "\t\t{\n\t\t\tMethodName: %q,\n\t\t\tHandler: %s,\n\t\t},\n", fn.Name, handlerName); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(g.out, "\t},\n}\n\n"); err != nil {
		return err
	}

	// Generate individual handler functions
	for _, fn := range funcs {
		if fn.IsTemplate {
			continue
		}
		if err := g.generateHandlerFunction(name, fn); err != nil {
			return err
		}
	}

	return nil
}

// generateHandlerFunction emits a static handler function for a method.
func (g *ServiceGenerator) generateHandlerFunction(serviceName string, fn parser.FuncDecl) error {
	method := g.namer.MethodName(fn.Name)
	reqType := g.namer.RequestName(fn.Name)
	handlerName := fmt.Sprintf("_%s_%s_Handler", serviceName, method)

	if _, err := fmt.Fprintf(g.out, "// %s is the static handler for %s.%s\nfunc %s(srv interface{}, ctx context.Context, req interface{}) (interface{}, error) {\n\treturn srv.(%s).%s(ctx, req.(*%s))\n}\n\n", handlerName, serviceName, method, handlerName, serviceName, method, reqType); err != nil {
		return err
	}

	return nil
}

// generateRegistrationFunction emits the simplified registration function.
func (g *ServiceGenerator) generateRegistrationFunction(service string, funcs []parser.FuncDecl) error {
	name := g.namer.ServiceName(service)
	descName := name + "_ServiceDesc"

	if _, err := fmt.Fprintf(g.out, "// Register%s registers the %s server with the TLRPC server.\nfunc Register%s(s *tlrpc.Server, srv %s) {\n", name, name, name, name); err != nil {
		return err
	}

	// Add the embedded check like gRPC does
	if _, err := fmt.Fprintf(g.out, "\t// If the following call panics, it indicates Unimplemented%s was\n\t// embedded by pointer and is nil. This will cause panics if an\n\t// unimplemented method is ever invoked, so we test this at initialization\n\t// time to prevent it from happening at runtime later due to I/O.\n\tif t, ok := srv.(interface{ testEmbeddedByValue() }); ok {\n\t\tt.testEmbeddedByValue()\n\t}\n", name); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(g.out, "\ts.RegisterService(%s, srv)\n}\n\n", descName); err != nil {
		return err
	}

	return nil
}

// GenerateRequests emits request structs for functions.
func (g *ServiceGenerator) GenerateRequests(funcs []parser.FuncDecl) error {
	services := groupByService(funcs)
	serviceNames := sortedKeys(services)
	for _, service := range serviceNames {
		for _, fn := range services[service] {
			if fn.IsTemplate {
				continue
			}
			reqName := g.namer.RequestName(fn.Name)
			if _, err := fmt.Fprintf(g.out, "type %s struct {\n", reqName); err != nil {
				return err
			}
			for _, param := range fn.Params {
				if shouldSkipParam(param) {
					continue
				}
				fieldName := g.namer.FieldName(param.Name)
				fieldType := g.goType(param.Type)
				if _, err := fmt.Fprintf(g.out, "\t%s %s\n", fieldName, fieldType); err != nil {
					return err
				}
			}
			if _, err := io.WriteString(g.out, "}\n\n"); err != nil {
				return err
			}

			if err := g.generateRequestMethods(fn, strings.TrimSuffix(reqName, "Request")); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *ServiceGenerator) generateRequestMethods(fn parser.FuncDecl, reqBaseName string) error {
	if _, err := fmt.Fprintf(g.out, "func (r *%sRequest) ConstructorID() uint32 { return 0x%08x }\n", reqBaseName, fn.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "func (r *%sRequest) Method() string { return %q }\n\n", reqBaseName, fn.Name); err != nil {
		return err
	}

	if err := g.generateRequestSerialize(fn, reqBaseName); err != nil {
		return err
	}
	if err := g.generateRequestDeserialize(fn, reqBaseName); err != nil {
		return err
	}
	return nil
}

func (g *ServiceGenerator) generateRequestSerialize(fn parser.FuncDecl, reqBaseName string) error {
	if _, err := fmt.Fprintf(g.out, "func (r *%sRequest) SerializeTL(w io.Writer) error {\n", reqBaseName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "\tif err := mtproto.WriteUint32(w, r.ConstructorID()); err != nil {\n\t\treturn err\n\t}\n"); err != nil {
		return err
	}
	for _, param := range fn.Params {
		if shouldSkipParam(param) {
			continue
		}
		fieldName := g.namer.FieldName(param.Name)
		if err := g.writeRequestSerializeField(param, fieldName); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\treturn nil\n}\n\n"); err != nil {
		return err
	}
	return nil
}

func (g *ServiceGenerator) writeRequestSerializeField(param parser.Parameter, fieldName string) error {
	typeRef := param.Type
	if typeRef.IsVector && typeRef.Generic != nil {
		if _, err := fmt.Fprintf(g.out, "\tif err := mtproto.WriteVectorHeader(w, len(r.%s)); err != nil {\n\t\treturn err\n\t}\n", fieldName); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "\tfor i := range r.%s {\n", fieldName); err != nil {
			return err
		}
		if err := g.writeRequestSerializeValue(*typeRef.Generic, fmt.Sprintf("r.%s[i]", fieldName), "\t\t"); err != nil {
			return err
		}
		if _, err := io.WriteString(g.out, "\t}\n"); err != nil {
			return err
		}
		return nil
	}
	return g.writeRequestSerializeValue(typeRef, "r."+fieldName, "\t")
}

func (g *ServiceGenerator) writeRequestSerializeValue(t parser.TypeRef, value, indent string) error {
	writeCall, ok := serializeBuiltinCall(t, value)
	if ok {
		_, err := fmt.Fprintf(g.out, "%s%s\n", indent, writeCall)
		return err
	}
	_, err := fmt.Fprintf(g.out, "%sif err := %s.SerializeTL(w); err != nil {\n%s\treturn err\n%s}\n", indent, value, indent, indent)
	return err
}

func (g *ServiceGenerator) generateRequestDeserialize(fn parser.FuncDecl, reqBaseName string) error {
	if _, err := fmt.Fprintf(g.out, "func (r *%sRequest) DeserializeTL(rd io.Reader) error {\n", reqBaseName); err != nil {
		return err
	}
	if _, err := io.WriteString(g.out, "\tctorID, err := mtproto.ReadUint32(rd)\n\tif err != nil {\n\t\treturn err\n\t}\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(g.out, "\tif ctorID != r.ConstructorID() {\n\t\treturn fmt.Errorf(\"wrong constructor: got %%x, want %%x\", ctorID, r.ConstructorID())\n\t}\n"); err != nil {
		return err
	}
	for _, param := range fn.Params {
		if shouldSkipParam(param) {
			continue
		}
		fieldName := g.namer.FieldName(param.Name)
		if err := g.writeRequestDeserializeField(param, fieldName); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(g.out, "\treturn nil\n}\n\n"); err != nil {
		return err
	}
	return nil
}

func (g *ServiceGenerator) writeRequestDeserializeField(param parser.Parameter, fieldName string) error {
	typeRef := param.Type
	if typeRef.IsVector && typeRef.Generic != nil {
		elementType := g.goBaseType(*typeRef.Generic)
		if _, err := fmt.Fprintf(g.out, "\tvar items []%s\n", elementType); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "\tif err := mtproto.ReadVector(rd, func() error {\n"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "\t\tvar item %s\n", elementType); err != nil {
			return err
		}
		if err := g.writeRequestDeserializeValue(*typeRef.Generic, "item"); err != nil {
			return err
		}
		if _, err := io.WriteString(g.out, "\t\titems = append(items, item)\n\t\treturn nil\n\t}); err != nil {\n\t\treturn err\n\t}\n"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "\tr.%s = items\n", fieldName); err != nil {
			return err
		}
		return nil
	}
	return g.writeRequestDeserializeValue(typeRef, "r."+fieldName)
}

func (g *ServiceGenerator) writeRequestDeserializeValue(t parser.TypeRef, target string) error {
	readCall, ok := deserializeBuiltinCallWithReader(t, "rd")
	if ok {
		if _, err := fmt.Fprintf(g.out, "\t{\n\t\tvalue, err := %s\n\t\tif err != nil {\n\t\t\treturn err\n\t\t}\n", readCall); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.out, "\t\t%s = value\n\t}\n", target); err != nil {
			return err
		}
		return nil
	}
	if _, err := fmt.Fprintf(g.out, "\tif err := %s.DeserializeTL(rd); err != nil {\n\t\treturn err\n\t}\n", target); err != nil {
		return err
	}
	return nil
}

func (g *ServiceGenerator) responseType(t parser.TypeRef) string {
	base := g.goBaseType(t)
	// Interface types (union types ending with "Type") should not be pointers
	if strings.HasSuffix(base, "Type") {
		return base
	}
	if shouldPointerReturn(t) {
		return "*" + base
	}
	return base
}

func shouldPointerReturn(t parser.TypeRef) bool {
	if t.IsVector {
		return false
	}
	if t.Namespace != "" {
		return true
	}
	switch t.Name {
	case "int", "long", "int128", "int256", "double", "string", "bytes", "Bool", "bool", "true", "false", "#":
		return false
	default:
		return true
	}
}

func (g *ServiceGenerator) goType(t parser.TypeRef) string {
	base := g.goBaseType(t)
	if t.Optional && !isTrueType(t) {
		return "*" + base
	}
	return base
}

func (g *ServiceGenerator) goBaseType(t parser.TypeRef) string {
	if t.IsVector && t.Generic != nil {
		return "[]" + g.goBaseType(*t.Generic)
	}

	// Check if this is a union type (interface)
	// For service return types, we need to check the transformed Go type name
	goTypeName := g.namer.TypeName(t.Name)
	if t.Namespace != "" {
		goTypeName = g.namer.TypeName(t.Namespace + "." + t.Name)
	}

	unionTypes := map[string]bool{
		"Bool": true, "InputPeer": true, "InputUser": true, "InputFile": true, "InputMedia": true,
		"InputChatPhoto": true, "InputGeoPoint": true, "InputPhoto": true, "InputFileLocation": true,
		"Peer": true, "StorageFileType": true, "User": true, "UserProfilePhoto": true, "UserStatus": true,
		"Chat": true, "ChatFull": true, "ChatParticipant": true, "ChatParticipants": true, "ChatPhoto": true,
		"Message": true, "MessageMedia": true, "MessageAction": true, "Dialog": true, "Photo": true,
		"PhotoSize": true, "GeoPoint": true, "AuthAuthorization": true, "InputNotifyPeer": true,
		"WallPaper": true, "ReportReason": true, "ContactsContacts": true, "ContactsBlocked": true,
		"MessagesDialogs": true, "MessagesMessages": true, "MessagesChats": true, "MessagesFilter": true,
		"Update": true, "UpdatesDifference": true, "Updates": true, "PhotosPhotos": true, "UploadFile": true,
		"HelpAppUpdate": true, "EncryptedChat": true, "EncryptedFile": true, "InputEncryptedFile": true,
		"EncryptedMessage": true, "MessagesDhConfig": true, "MessagesSentEncryptedMessage": true,
		"InputDocument": true, "Document": true, "NotifyPeer": true, "SendMessageAction": true,
		"InputPrivacyKey": true, "PrivacyKey": true, "InputPrivacyRule": true, "PrivacyRule": true,
		"DocumentAttribute": true, "MessagesStickers": true, "MessagesAllStickers": true, "WebPage": true,
		"ExportedChatInvite": true, "ChatInvite": true, "InputStickerSet": true, "MessagesStickerSet": true,
		"KeyboardButton": true, "ReplyMarkup": true, "MessageEntity": true, "InputChannel": true,
		"UpdatesChannelDifference": true, "ChannelMessagesFilter": true, "ChannelParticipant": true,
		"ChannelParticipantsFilter": true, "ChannelsChannelParticipants": true, "MessagesSavedGifs": true,
		"InputBotInlineMessage": true, "InputBotInlineResult": true, "BotInlineMessage": true,
		"BotInlineResult": true, "AuthCodeType": true, "AuthSentCodeType": true, "InputBotInlineMessageID": true,
		"TopPeerCategory": true, "ContactsTopPeers": true, "DraftMessage": true, "MessagesFeaturedStickers": true,
		"MessagesRecentStickers": true, "MessagesStickerSetInstallResult": true, "StickerSetCovered": true,
		"InputStickeredMedia": true, "InputGame": true, "RichText": true, "PageBlock": true,
		"PhoneCallDiscardReason": true, "WebDocument": true, "InputWebFileLocation": true,
		"PaymentsPaymentResult": true, "InputPaymentCredentials": true, "PhoneCall": true,
		"PhoneConnection": true, "UploadCdnFile": true, "LangPackString": true, "ChannelAdminLogEventAction": true,
		"MessagesFavedStickers": true, "RecentMeURL": true, "InputMessage": true, "InputDialogPeer": true,
		"DialogPeer": true, "MessagesFoundStickerSets": true, "HelpTermsOfServiceUpdate": true,
		"InputSecureFile": true, "SecureFile": true, "SecurePlainData": true, "SecureValueType": true,
		"SecureValueError": true, "HelpDeepLinkInfo": true, "PasswordKdfAlgo": true, "SecurePasswordKdfAlgo": true,
		"InputCheckPasswordSrp": true, "SecureRequiredType": true, "HelpPassportConfig": true, "JSONValue": true,
		"PageListItem": true, "PageListOrderedItem": true, "HelpUserInfo": true, "InputWallPaper": true,
		"AccountWallPapers": true, "EmojiKeyword": true, "URLAuthResult": true, "ChannelLocation": true,
		"PeerLocated": true, "InputTheme": true, "AccountThemes": true, "AuthLoginToken": true, "BaseTheme": true,
		"MessageUserVote": true, "DialogFilter": true, "StatsGraph": true, "HelpPromoData": true,
		"HelpCountriesList": true, "GroupCall": true, "InlineQueryPeerType": true, "MessagesExportedChatInvite": true,
		"BotCommandScope": true, "AccountResetPasswordResult": true, "MessagesAvailableReactions": true,
		"MessagesTranslatedText": true, "AttachMenuBots": true, "BotMenuButton": true, "AccountSavedRingtones": true,
		"NotificationSound": true, "AccountSavedRingtone": true, "AttachMenuPeerType": true, "InputInvoice": true,
		"InputStorePaymentPurpose": true, "EmojiStatus": true, "AccountEmojiStatuses": true, "Reaction": true,
		"ChatReactions": true, "MessagesReactions": true, "EmailVerifyPurpose": true, "EmailVerification": true,
		"AccountEmailVerified": true,
	}
	if unionTypes[goTypeName] {
		return goTypeName + "Type"
	}

	if t.Namespace != "" {
		return g.namer.TypeName(t.Namespace + "." + t.Name)
	}

	// For service return types, don't include namespace prefix
	// This matches how the type generator creates union types
	switch t.Name {
	case "int":
		return "int32"
	case "long":
		return "int64"
	case "int128":
		return "[16]byte"
	case "int256":
		return "[32]byte"
	case "double":
		return "float64"
	case "string":
		return "string"
	case "bytes":
		return "[]byte"
	case "Bool", "bool", "true", "false":
		return "bool"
	case "#":
		return "uint32"
	default:
		// For union types, use the interface name
		unionTypes := map[string]bool{
			"Bool": true, "InputPeer": true, "InputUser": true, "InputFile": true, "InputMedia": true,
			"InputChatPhoto": true, "InputGeoPoint": true, "InputPhoto": true, "InputFileLocation": true,
			"Peer": true, "StorageFileType": true, "User": true, "UserProfilePhoto": true, "UserStatus": true,
			"Chat": true, "ChatFull": true, "ChatParticipant": true, "ChatParticipants": true, "ChatPhoto": true,
			"Message": true, "MessageMedia": true, "MessageAction": true, "Dialog": true, "Photo": true,
			"PhotoSize": true, "GeoPoint": true, "AuthAuthorization": true, "InputNotifyPeer": true,
			"WallPaper": true, "ReportReason": true, "ContactsContacts": true, "ContactsBlocked": true,
			"MessagesDialogs": true, "MessagesMessages": true, "MessagesChats": true, "MessagesFilter": true,
			"Update": true, "UpdatesDifference": true, "Updates": true, "PhotosPhotos": true, "UploadFile": true,
			"HelpAppUpdate": true, "EncryptedChat": true, "EncryptedFile": true, "InputEncryptedFile": true,
			"EncryptedMessage": true, "MessagesDhConfig": true, "MessagesSentEncryptedMessage": true,
			"InputDocument": true, "Document": true, "NotifyPeer": true, "SendMessageAction": true,
			"InputPrivacyKey": true, "PrivacyKey": true, "InputPrivacyRule": true, "PrivacyRule": true,
			"DocumentAttribute": true, "MessagesStickers": true, "MessagesAllStickers": true, "WebPage": true,
			"ExportedChatInvite": true, "ChatInvite": true, "InputStickerSet": true, "MessagesStickerSet": true,
			"KeyboardButton": true, "ReplyMarkup": true, "MessageEntity": true, "InputChannel": true,
			"UpdatesChannelDifference": true, "ChannelMessagesFilter": true, "ChannelParticipant": true,
			"ChannelParticipantsFilter": true, "ChannelsChannelParticipants": true, "MessagesSavedGifs": true,
			"InputBotInlineMessage": true, "InputBotInlineResult": true, "BotInlineMessage": true,
			"BotInlineResult": true, "AuthCodeType": true, "AuthSentCodeType": true, "InputBotInlineMessageID": true,
			"TopPeerCategory": true, "ContactsTopPeers": true, "DraftMessage": true, "MessagesFeaturedStickers": true,
			"MessagesRecentStickers": true, "MessagesStickerSetInstallResult": true, "StickerSetCovered": true,
			"InputStickeredMedia": true, "InputGame": true, "RichText": true, "PageBlock": true,
			"PhoneCallDiscardReason": true, "WebDocument": true, "InputWebFileLocation": true,
			"PaymentsPaymentResult": true, "InputPaymentCredentials": true, "PhoneCall": true,
			"PhoneConnection": true, "UploadCdnFile": true, "LangPackString": true, "ChannelAdminLogEventAction": true,
			"MessagesFavedStickers": true, "RecentMeURL": true, "InputMessage": true, "InputDialogPeer": true,
			"DialogPeer": true, "MessagesFoundStickerSets": true, "HelpTermsOfServiceUpdate": true,
			"InputSecureFile": true, "SecureFile": true, "SecurePlainData": true, "SecureValueType": true,
			"SecureValueError": true, "HelpDeepLinkInfo": true, "PasswordKdfAlgo": true, "SecurePasswordKdfAlgo": true,
			"InputCheckPasswordSrp": true, "SecureRequiredType": true, "HelpPassportConfig": true, "JSONValue": true,
			"PageListItem": true, "PageListOrderedItem": true, "HelpUserInfo": true, "InputWallPaper": true,
			"AccountWallPapers": true, "EmojiKeyword": true, "URLAuthResult": true, "ChannelLocation": true,
			"PeerLocated": true, "InputTheme": true, "AccountThemes": true, "AuthLoginToken": true, "BaseTheme": true,
			"MessageUserVote": true, "DialogFilter": true, "StatsGraph": true, "HelpPromoData": true,
			"HelpCountriesList": true, "GroupCall": true, "InlineQueryPeerType": true, "MessagesExportedChatInvite": true,
			"BotCommandScope": true, "AccountResetPasswordResult": true, "MessagesAvailableReactions": true,
			"MessagesTranslatedText": true, "AttachMenuBots": true, "BotMenuButton": true, "AccountSavedRingtones": true,
			"NotificationSound": true, "AccountSavedRingtone": true, "AttachMenuPeerType": true, "InputInvoice": true,
			"InputStorePaymentPurpose": true, "EmojiStatus": true, "AccountEmojiStatuses": true, "Reaction": true,
			"ChatReactions": true, "MessagesReactions": true, "EmailVerifyPurpose": true, "EmailVerification": true,
			"AccountEmailVerified": true,
		}
		if unionTypes[t.Name] {
			return g.namer.TypeName(t.Name) + "Type"
		}
		return g.namer.TypeName(t.Name)
	}
}

func zeroValue(typ string) string {
	if strings.HasPrefix(typ, "*") {
		return "nil"
	}
	switch typ {
	case "string":
		return "\"\""
	case "bool":
		return "false"
	case "int32", "int64", "uint32", "uint64", "float64":
		return "0"
	default:
		if strings.HasPrefix(typ, "[]") {
			return "nil"
		}
		return typ + "{}"
	}
}

func groupByService(funcs []parser.FuncDecl) map[string][]parser.FuncDecl {
	services := make(map[string][]parser.FuncDecl)
	for _, fn := range funcs {
		parts := strings.Split(fn.Name, ".")
		service := ""
		if len(parts) > 1 {
			service = parts[0]
		} else {
			service = "root"
		}
		services[service] = append(services[service], fn)
	}
	for name := range services {
		sort.Slice(services[name], func(i, j int) bool {
			return services[name][i].Name < services[name][j].Name
		})
	}
	return services
}

func sortedKeys(m map[string][]parser.FuncDecl) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
