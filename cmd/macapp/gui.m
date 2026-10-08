#import <Cocoa/Cocoa.h>
#import "gui.h"

extern void goOnCopyRefreshToken();
extern void goOnCopyDockerCompose();
extern void goOnCopyBaseURL();
extern void goOnLoginRequested();
extern void goOnSendPrompt(const char* prompt, const char* model);

@interface MainWindowController : NSObject <NSWindowDelegate>
@property (strong) NSWindow *window;
@property (strong) NSTextField *statusLabel;
@property (strong) NSTextField *emailLabel;
@property (strong) NSTextField *projectLabel;
@property (strong) NSTextField *tokenLabel;
@property (strong) NSTextField *noticeLabel;
@property (strong) NSPopUpButton *modelPopup;
@property (strong) NSTextField *inputField;
@property (strong) NSButton *sendButton;
@property (strong) NSTextView *outputView;
@property (assign) int port;
@property (copy) NSString *refreshToken;

- (void)onCopyRefreshToken:(id)sender;
- (void)onCopyDockerCompose:(id)sender;
- (void)onCopyBaseURL:(id)sender;
- (void)onLogin:(id)sender;
- (void)onSend:(id)sender;
- (void)showNotice:(NSString *)msg;
@end

static MainWindowController *gController = nil;

@implementation MainWindowController

- (void)buildUI {
    NSRect frame = NSMakeRect(0, 0, 680, 640);
    self.window = [[NSWindow alloc] initWithContentRect:frame
                                              styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable)
                                                backing:NSBackingStoreBuffered
                                                  defer:NO];
    [self.window setTitle:@"🪐 Antigravity2Api - 桌面控制台"];
    [self.window center];
    [self.window setDelegate:self];

    NSView *root = [self.window contentView];

    // 1. Header
    NSTextField *title = [[NSTextField alloc] initWithFrame:NSMakeRect(24, 580, 400, 32)];
    [title setStringValue:@"🪐 Antigravity2Api"];
    [title setFont:[NSFont boldSystemFontOfSize:22]];
    [title setBezeled:NO];
    [title setDrawsBackground:NO];
    [title setEditable:NO];
    [root addSubview:title];

    NSTextField *sub = [[NSTextField alloc] initWithFrame:NSMakeRect(26, 560, 450, 18)];
    [sub setStringValue:@"OpenAI 兼容协议网关 (Gemini 3.8 / 3.7 / 3.1 & Claude)"];
    [sub setFont:[NSFont systemFontOfSize:12]];
    [sub setTextColor:[NSColor secondaryLabelColor]];
    [sub setBezeled:NO];
    [sub setDrawsBackground:NO];
    [sub setEditable:NO];
    [root addSubview:sub];

    // Notice Label (for clipboard feedback)
    self.noticeLabel = [[NSTextField alloc] initWithFrame:NSMakeRect(400, 585, 250, 24)];
    [self.noticeLabel setAlignment:NSTextAlignmentRight];
    [self.noticeLabel setFont:[NSFont boldSystemFontOfSize:12]];
    [self.noticeLabel setTextColor:[NSColor systemGreenColor]];
    [self.noticeLabel setBezeled:NO];
    [self.noticeLabel setDrawsBackground:NO];
    [self.noticeLabel setEditable:NO];
    [root addSubview:self.noticeLabel];

    // 2. Info Box (Box)
    NSBox *box = [[NSBox alloc] initWithFrame:NSMakeRect(24, 430, 632, 120)];
    [box setTitle:@"📡 服务状态与账号信息"];
    [box setBoxType:NSBoxPrimary];

    NSView *boxView = [box contentView];

    self.statusLabel = [[NSTextField alloc] initWithFrame:NSMakeRect(16, 70, 300, 20)];
    [self.statusLabel setFont:[NSFont systemFontOfSize:13 weight:NSFontWeightMedium]];
    [self.statusLabel setBezeled:NO];
    [self.statusLabel setDrawsBackground:NO];
    [self.statusLabel setEditable:NO];
    [boxView addSubview:self.statusLabel];

    self.emailLabel = [[NSTextField alloc] initWithFrame:NSMakeRect(16, 44, 300, 20)];
    [self.emailLabel setFont:[NSFont systemFontOfSize:13]];
    [self.emailLabel setBezeled:NO];
    [self.emailLabel setDrawsBackground:NO];
    [self.emailLabel setEditable:NO];
    [boxView addSubview:self.emailLabel];

    self.projectLabel = [[NSTextField alloc] initWithFrame:NSMakeRect(16, 18, 300, 20)];
    [self.projectLabel setFont:[NSFont systemFontOfSize:13]];
    [self.projectLabel setBezeled:NO];
    [self.projectLabel setDrawsBackground:NO];
    [self.projectLabel setEditable:NO];
    [boxView addSubview:self.projectLabel];

    self.tokenLabel = [[NSTextField alloc] initWithFrame:NSMakeRect(340, 70, 270, 20)];
    [self.tokenLabel setFont:[NSFont systemFontOfSize:13]];
    [self.tokenLabel setBezeled:NO];
    [self.tokenLabel setDrawsBackground:NO];
    [self.tokenLabel setEditable:NO];
    [boxView addSubview:self.tokenLabel];

    [root addSubview:box];

    // 3. Action Buttons
    CGFloat btnY = 385;
    NSButton *btnCopyToken = [NSButton buttonWithTitle:@"📋 复制 Refresh Token (供 Docker)" target:self action:@selector(onCopyRefreshToken:)];
    [btnCopyToken setFrame:NSMakeRect(24, btnY, 240, 32)];
    [btnCopyToken setBezelStyle:NSBezelStyleRounded];
    [root addSubview:btnCopyToken];

    NSButton *btnCopyCompose = [NSButton buttonWithTitle:@"📋 复制 Docker Compose 配置" target:self action:@selector(onCopyDockerCompose:)];
    [btnCopyCompose setFrame:NSMakeRect(270, btnY, 210, 32)];
    [btnCopyCompose setBezelStyle:NSBezelStyleRounded];
    [root addSubview:btnCopyCompose];

    NSButton *btnCopyBase = [NSButton buttonWithTitle:@"📋 复制 Base URL" target:self action:@selector(onCopyBaseURL:)];
    [btnCopyBase setFrame:NSMakeRect(486, btnY, 170, 32)];
    [btnCopyBase setBezelStyle:NSBezelStyleRounded];
    [root addSubview:btnCopyBase];

    // 4. Test Area Box
    NSBox *testBox = [[NSBox alloc] initWithFrame:NSMakeRect(24, 20, 632, 350)];
    [testBox setTitle:@"🧪 原生对话测试 (直接在桌面测试 API)"];
    [testBox setBoxType:NSBoxPrimary];
    NSView *tView = [testBox contentView];

    NSTextField *mLabel = [[NSTextField alloc] initWithFrame:NSMakeRect(16, 290, 40, 22)];
    [mLabel setStringValue:@"模型:"];
    [mLabel setBezeled:NO];
    [mLabel setDrawsBackground:NO];
    [mLabel setEditable:NO];
    [tView addSubview:mLabel];

    self.modelPopup = [[NSPopUpButton alloc] initWithFrame:NSMakeRect(60, 288, 260, 26) pullsDown:NO];
    [self.modelPopup addItemsWithTitles:@[
        @"gemini-3.8-flash (最新主力)",
        @"gemini-3.7-flash",
        @"gemini-3.6-flash",
        @"gemini-3.1-pro",
        @"claude-sonnet-4-6",
        @"gpt-4o"
    ]];
    [tView addSubview:self.modelPopup];

    NSButton *btnLogin = [NSButton buttonWithTitle:@"🔑 重新登录授权" target:self action:@selector(onLogin:)];
    [btnLogin setFrame:NSMakeRect(490, 287, 130, 28)];
    [btnLogin setBezelStyle:NSBezelStyleRounded];
    [tView addSubview:btnLogin];

    self.inputField = [[NSTextField alloc] initWithFrame:NSMakeRect(16, 248, 500, 30)];
    [self.inputField setPlaceholderString:@"输入测试提示词，例如：9.11 和 9.9 哪个大？或写一首短诗..."];
    [self.inputField setTarget:self];
    [self.inputField setAction:@selector(onSend:)];
    [tView addSubview:self.inputField];

    self.sendButton = [NSButton buttonWithTitle:@"发送测试" target:self action:@selector(onSend:)];
    [self.sendButton setFrame:NSMakeRect(520, 247, 98, 32)];
    [self.sendButton setBezelStyle:NSBezelStyleRounded];
    [tView addSubview:self.sendButton];

    // Output scroll & view
    NSScrollView *scroll = [[NSScrollView alloc] initWithFrame:NSMakeRect(16, 16, 600, 220)];
    [scroll setHasVerticalScroller:YES];
    [scroll setBorderType:NSBezelBorder];

    self.outputView = [[NSTextView alloc] initWithFrame:scroll.bounds];
    [self.outputView setEditable:NO];
    [self.outputView setFont:[NSFont userFixedPitchFontOfSize:12]];
    [self.outputView setString:@"等待输入测试...\n支持流式思考过程与回答展示。"];
    [scroll setDocumentView:self.outputView];
    [tView addSubview:scroll];

    [root addSubview:testBox];
}

- (void)updateWithState:(AppState)state {
    self.port = state.port;
    if (state.refreshToken) {
        self.refreshToken = [NSString stringWithUTF8String:state.refreshToken];
    }

    NSString *st = state.authenticated ? [NSString stringWithFormat:@"🟢 运行中 (http://localhost:%d)", state.port] : @"🟡 未授权 Google";
    [self.statusLabel setStringValue:st];

    NSString *em = state.userEmail ? [NSString stringWithFormat:@"账号: %s", state.userEmail] : @"账号: 未绑定";
    [self.emailLabel setStringValue:em];

    NSString *pid = state.projectID ? [NSString stringWithFormat:@"项目: %s", state.projectID] : @"项目: 未获取";
    [self.projectLabel setStringValue:pid];

    NSString *tok = state.authenticated ? @"Token: 有效 (自动续期)" : @"Token: 需登录";
    [self.tokenLabel setStringValue:tok];
}

- (void)showNotice:(NSString *)msg {
    [self.noticeLabel setStringValue:msg];
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(3 * NSEC_PER_SEC)), dispatch_get_main_queue(), ^{
        [self.noticeLabel setStringValue:@""];
    });
}

- (void)onCopyRefreshToken:(id)sender {
    goOnCopyRefreshToken();
    [self showNotice:@"✅ 已复制 Refresh Token！"];
}

- (void)onCopyDockerCompose:(id)sender {
    goOnCopyDockerCompose();
    [self showNotice:@"✅ 已复制 Docker 配置！"];
}

- (void)onCopyBaseURL:(id)sender {
    goOnCopyBaseURL();
    [self showNotice:@"✅ 已复制 Base URL！"];
}

- (void)onLogin:(id)sender {
    goOnLoginRequested();
    [self showNotice:@"正在打开 Google 授权..."];
}

- (void)onSend:(id)sender {
    NSString *text = [self.inputField stringValue];
    if ([text length] == 0) return;

    NSString *modelTitle = [self.modelPopup titleOfSelectedItem];
    NSString *model = [[modelTitle componentsSeparatedByString:@" "] firstObject];

    [self.outputView setString:@"正在向模型发送请求并接收回复...\n\n"];
    [self.sendButton setEnabled:NO];

    dispatch_async(dispatch_get_global_queue(DISPATCH_QUEUE_PRIORITY_DEFAULT, 0), ^{
        goOnSendPrompt([text UTF8String], [model UTF8String]);
        dispatch_async(dispatch_get_main_queue(), ^{
            [self.sendButton setEnabled:YES];
        });
    });
}

- (void)appendOutput:(NSString *)text {
    dispatch_async(dispatch_get_main_queue(), ^{
        [self.outputView setString:text];
        [self.outputView scrollRangeToVisible:NSMakeRange([[self.outputView string] length], 0)];
    });
}

- (BOOL)windowShouldClose:(NSWindow *)sender {
    // Hide window when closing, keep running in dock
    [self.window orderOut:nil];
    return NO;
}

@end

void runMacGUI(AppState state) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        // Regular app policy so it shows normally in Dock / taskbar
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];

        gController = [[MainWindowController alloc] init];
        [gController buildUI];
        [gController updateWithState:state];

        [gController.window makeKeyAndOrderFront:nil];
        [NSApp activateIgnoringOtherApps:YES];

        [NSApp run];
    }
}

void updateGUIState(AppState state) {
    if (gController) {
        dispatch_async(dispatch_get_main_queue(), ^{
            [gController updateWithState:state];
        });
    }
}

void setGUIResponseText(const char* text) {
    if (gController && text) {
        NSString *str = [NSString stringWithUTF8String:text];
        [gController appendOutput:str];
    }
}
