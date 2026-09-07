// Telegram API layer 229 incremental difference from layer 228.
// Source: Telegram Android revision 62b56a07ca7e30e39f7fd00a6728d6bbd716ca1c.
// Snapshots: TMessagesProj_AppTests/tlscheme/{228,229}.json.
// The verifier checks this complete 30-add/17-remove/15-change constructor and
// 6-add/0-remove/4-change method difference by name, ID, result, and fields.

---types---

// Removed in layer 229.
// @tlrpc remove constructor inputKeyboardButtonRequestPeer
// @tlrpc remove constructor inputKeyboardButtonUrlAuth
// @tlrpc remove constructor inputKeyboardButtonUserProfile
// @tlrpc remove constructor keyboardButtonBuy
// @tlrpc remove constructor keyboardButtonCallback
// @tlrpc remove constructor keyboardButtonCopy
// @tlrpc remove constructor keyboardButtonGame
// @tlrpc remove constructor keyboardButtonRequestGeoLocation
// @tlrpc remove constructor keyboardButtonRequestPeer
// @tlrpc remove constructor keyboardButtonRequestPhone
// @tlrpc remove constructor keyboardButtonRequestPoll
// @tlrpc remove constructor keyboardButtonSimpleWebView
// @tlrpc remove constructor keyboardButtonSwitchInline
// @tlrpc remove constructor keyboardButtonUrl
// @tlrpc remove constructor keyboardButtonUrlAuth
// @tlrpc remove constructor keyboardButtonUserProfile
// @tlrpc remove constructor keyboardButtonWebView

auth.firebasePnvIntent#df5ac00c nonce:string digital_credential_payload:string = auth.FirebasePnvIntent;
buttonTypeDefault#c9dd90e9 = ButtonType;
buttonTypeRequestGeoLocation#9beee140 = ButtonType;
buttonTypeRequestPeer#4f58a237 flags:# button_id:int peer_type:RequestPeerType max_quantity:int = ButtonType;
buttonTypeRequestPhone#df3d36f9 = ButtonType;
buttonTypeRequestPoll#aacfff84 flags:# quiz:flags.0?Bool = ButtonType;
buttonTypeSimpleWebView#c01a597a url:string = ButtonType;
channelFull#a04e8d3a flags:# can_view_participants:flags.3?true can_set_username:flags.6?true can_set_stickers:flags.7?true hidden_prehistory:flags.10?true can_set_location:flags.16?true has_scheduled:flags.19?true can_view_stats:flags.20?true blocked:flags.22?true flags2:# can_delete_channel:flags2.0?true antispam:flags2.1?true participants_hidden:flags2.2?true translations_disabled:flags2.3?true stories_pinned_available:flags2.5?true view_forum_as_messages:flags2.6?true restricted_sponsored:flags2.11?true can_view_revenue:flags2.12?true paid_media_allowed:flags2.14?true can_view_stars_revenue:flags2.15?true paid_reactions_available:flags2.16?true stargifts_available:flags2.19?true paid_messages_available:flags2.20?true has_welcome_messages:flags2.24?true id:long about:string participants_count:flags.0?int admins_count:flags.1?int kicked_count:flags.2?int banned_count:flags.2?int online_count:flags.13?int read_inbox_max_id:int read_outbox_max_id:int unread_count:int chat_photo:Photo notify_settings:PeerNotifySettings exported_invite:flags.23?ExportedChatInvite bot_info:Vector<BotInfo> migrated_from_chat_id:flags.4?long migrated_from_max_id:flags.4?int pinned_msg_id:flags.5?int stickerset:flags.8?StickerSet available_min_id:flags.9?int folder_id:flags.11?int linked_chat_id:flags.14?long location:flags.15?ChannelLocation slowmode_seconds:flags.17?int slowmode_next_send_date:flags.18?int stats_dc:flags.12?int pts:int call:flags.21?InputGroupCall ttl_period:flags.24?int pending_suggestions:flags.25?Vector<string> groupcall_default_join_as:flags.26?Peer theme_emoticon:flags.27?string requests_pending:flags.28?int recent_requesters:flags.28?Vector<long> default_send_as:flags.29?Peer available_reactions:flags.30?ChatReactions reactions_limit:flags2.13?int stories:flags2.4?PeerStories wallpaper:flags2.7?WallPaper boosts_applied:flags2.8?int boosts_unrestrict:flags2.9?int emojiset:flags2.10?StickerSet bot_verification:flags2.17?BotVerification stargifts_count:flags2.18?int send_paid_messages_stars:flags2.21?long main_tab:flags2.22?ProfileTab guard_bot_id:flags2.23?long = ChatFull;
chatAdminRights#5fb224d5 flags:# change_info:flags.0?true post_messages:flags.1?true edit_messages:flags.2?true delete_messages:flags.3?true ban_users:flags.4?true invite_users:flags.5?true pin_messages:flags.7?true add_admins:flags.9?true anonymous:flags.10?true manage_call:flags.11?true other:flags.12?true manage_topics:flags.13?true post_stories:flags.14?true edit_stories:flags.15?true delete_stories:flags.16?true manage_direct_messages:flags.17?true manage_ranks:flags.18?true manage_linked_peers:flags.19?true manage_welcome_messages:flags.20?true = ChatAdminRights;
chatFull#2633421b flags:# can_set_username:flags.7?true has_scheduled:flags.8?true translations_disabled:flags.19?true has_welcome_messages:flags.21?true id:long about:string participants:ChatParticipants chat_photo:flags.2?Photo notify_settings:PeerNotifySettings exported_invite:flags.13?ExportedChatInvite bot_info:flags.3?Vector<BotInfo> pinned_msg_id:flags.6?int folder_id:flags.11?int call:flags.12?InputGroupCall ttl_period:flags.14?int groupcall_default_join_as:flags.15?Peer theme_emoticon:flags.16?string requests_pending:flags.17?int recent_requesters:flags.17?Vector<long> available_reactions:flags.18?ChatReactions reactions_limit:flags.20?int = ChatFull;
ephemeral.welcomeMessages#104fc872 hash:long messages:Vector<EphemeralMessage> = ephemeral.WelcomeMessages;
ephemeral.welcomeMessagesNotModified#59ffdb31 = ephemeral.WelcomeMessages;
ephemeralMessage#dd27bee9 flags:# out:flags.0?true welcome_template:flags.5?true invert_media:flags.7?true noforwards:flags.12?true id:int from_id:Peer peer_id:flags.9?Peer receiver_id:long top_msg_id:flags.1?int date:int message:string entities:flags.2?Vector<MessageEntity> media:flags.3?MessageMedia reply_markup:flags.4?ReplyMarkup reply_to:flags.6?MessageReplyHeader rich_message:flags.8?RichMessage chat_instance:flags.10?long anchor_msg_id:flags.11?int = EphemeralMessage;
inlineButtonTypeBuy#48bad7a5 = InlineButtonType;
inlineButtonTypeCallback#2955bc38 flags:# requires_password:flags.0?true data:bytes = InlineButtonType;
inlineButtonTypeCopy#b41d3272 copy_text:string = InlineButtonType;
inlineButtonTypeDisabled#a438619d = InlineButtonType;
inlineButtonTypeGame#5cd3709d = InlineButtonType;
inlineButtonTypeSwitchInline#93773ff5 flags:# same_peer:flags.0?true query:string peer_types:flags.1?Vector<InlineQueryPeerType> = InlineButtonType;
inlineButtonTypeUrl#eca4f8d4 url:string = InlineButtonType;
inlineButtonTypeUrlAuth#bfd02da2 flags:# fwd_text:flags.0?string url:string button_id:int = InlineButtonType;
inlineButtonTypeUserProfile#3fa33fcf user_id:long = InlineButtonType;
inlineButtonTypeWebView#3bcab5b4 url:string = InlineButtonType;
inputButtonTypeRequestPeer#3fe268fe flags:# name_requested:flags.0?true username_requested:flags.1?true photo_requested:flags.2?true button_id:int peer_type:RequestPeerType max_quantity:int = ButtonType;
inputInlineButtonTypeUrlAuth#9961bcb4 flags:# request_write_access:flags.0?true fwd_text:flags.1?string url:string bot:flags.2?InputUser = InlineButtonType;
inputInlineButtonTypeUserProfile#53f3ce5a user_id:InputUser = InlineButtonType;
inputInvoiceStarGiftResale#e9b0c658 flags:# ton:flags.0?true show_name:flags.2?true slug:string to_id:InputPeer message:flags.1?TextWithEntities = InputInvoice;
inputSendMessageRichMessageDraftAction#a937c7be flags:# can_stop:flags.0?true keep_on_stop:flags.1?true random_id:long rich_message:InputRichMessage = SendMessageAction;
keyboardButton#2f67a72f flags:# style:flags.10?KeyboardButtonStyle text:string type:ButtonType = KeyboardButton;
keyboardInlineButton#11c1a322 flags:# style:flags.10?KeyboardButtonStyle text:string type:InlineButtonType = KeyboardInlineButton;
keyboardInlineButtonRow#19420af6 buttons:Vector<KeyboardInlineButton> = KeyboardInlineButtonRow;
messageActionStarGiftUnique#7e1c1187 flags:# upgrade:flags.0?true transferred:flags.1?true saved:flags.2?true refunded:flags.5?true prepaid_upgrade:flags.11?true assigned:flags.13?true from_offer:flags.14?true craft:flags.16?true name_hidden:flags.17?true gift:StarGift can_export_at:flags.3?int transfer_stars:flags.4?long from_id:flags.6?Peer peer:flags.7?Peer saved_id:flags.7?long resale_amount:flags.8?StarsAmount can_transfer_at:flags.9?int can_resell_at:flags.10?int drop_original_details_stars:flags.12?long can_craft_at:flags.15?int message:flags.18?TextWithEntities = MessageAction;
pageBlockBlockquote#66d1670b flags:# collapsed:flags.0?true text:RichText caption:RichText = PageBlock;
pageBlockButtonRow#6d640318 flags:# align_left:flags.0?true align_center:flags.1?true align_right:flags.2?true buttons:Vector<PageButton> = PageBlock;
pageBlockDocument#38fa3ba3 document_id:long caption:PageCaption = PageBlock;
pageBlockTable#bf4dea82 flags:# bordered:flags.0?true striped:flags.1?true compact:flags.2?true title:RichText rows:Vector<PageTableRow> = PageBlock;
pageButton#692a5488 flags:# text:RichText type:InlineButtonType style:flags.0?RichButtonStyle = PageButton;
replyInlineMarkup#b2b15770 flags:# force_reply:flags.5?true rows:Vector<KeyboardInlineButtonRow> = ReplyMarkup;
replyKeyboardMarkup#85dd99d1 flags:# resize:flags.0?true single_use:flags.1?true selective:flags.2?true persistent:flags.4?true force_reply:flags.5?true rows:Vector<KeyboardButtonRow> placeholder:flags.3?string = ReplyMarkup;
richButtonStyle#03c610bd flags:# bg_primary:flags.0?true bg_danger:flags.1?true bg_success:flags.2?true link:flags.3?true = RichButtonStyle;
sendMessageRichMessageDraftAction#52564893 flags:# can_stop:flags.0?true keep_on_stop:flags.1?true random_id:long rich_message:RichMessage = SendMessageAction;
sendMessageStopDraftAction#fbf902b0 random_id:long = SendMessageAction;
sendMessageTextDraftAction#3630b85a flags:# can_stop:flags.0?true keep_on_stop:flags.1?true random_id:long text:TextWithEntities = SendMessageAction;
textButton#afc79cd6 flags:# text:RichText type:InlineButtonType style:flags.0?RichButtonStyle = RichText;
updateEphemeralBotCallbackQuery#7c1079d6 flags:# query_id:long user_id:long peer:flags.0?Peer msg_id:int data:bytes chat_instance:flags.1?long message:EphemeralMessage = Update;

---functions---

auth.finishFirebasePnvLogin#2c85094c google_token:string = auth.Authorization;
auth.firebasePnvSignUp#783f6b56 flags:# no_joined_notifications:flags.0?true first_name:string last_name:string = auth.Authorization;
auth.initFirebasePnvLogin#777df37a api_id:int api_hash:string = auth.FirebasePnvIntent;
ephemeral.deleteAllWelcomeMessages#734f9721 peer:InputPeer = Bool;
ephemeral.deleteMessage#92f6e797 flags:# peer:flags.0?InputPeer receiver_id:InputUser id:int = Bool;
ephemeral.deleteWelcomeMessage#e882a9e1 peer:InputPeer id:int = Bool;
ephemeral.editMessage#cf9c725b flags:# invert_media:flags.5?true welcome:flags.6?true peer:flags.7?InputPeer receiver_id:InputUser id:int message:flags.0?string media:flags.3?InputMedia entities:flags.1?Vector<MessageEntity> reply_markup:flags.2?ReplyMarkup rich_message:flags.4?InputRichMessage = Updates;
ephemeral.getWelcomeMessages#db9ac18d peer:InputPeer hash:long = ephemeral.WelcomeMessages;
ephemeral.sendMessage#ba8d5f35 flags:# invert_media:flags.6?true welcome:flags.7?true anchor:flags.9?true noforwards:flags.10?true peer:flags.8?InputPeer receiver_id:InputUser query_id:flags.0?long message:string entities:flags.1?Vector<MessageEntity> media:flags.2?InputMedia reply_markup:flags.3?ReplyMarkup rich_message:flags.4?InputRichMessage random_id:long reply_to:flags.5?InputReplyTo = Updates;
messages.forwardMessages#13704a7c flags:# silent:flags.5?true background:flags.6?true with_my_score:flags.8?true drop_author:flags.11?true drop_media_captions:flags.12?true noforwards:flags.14?true allow_paid_floodskip:flags.19?true from_ephemeral:flags.25?true from_peer:InputPeer id:Vector<int> random_id:Vector<long> to_peer:InputPeer top_msg_id:flags.9?int reply_to:flags.22?InputReplyTo schedule_date:flags.10?int schedule_repeat_period:flags.24?int send_as:flags.13?InputPeer quick_reply_shortcut:flags.17?InputQuickReplyShortcut effect:flags.18?long video_timestamp:flags.20?int allow_paid_stars:flags.21?long suggested_post:flags.23?SuggestedPost = Updates;
