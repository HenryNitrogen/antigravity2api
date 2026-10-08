#ifndef GUI_H
#define GUI_H

typedef struct {
    int port;
    const char* userEmail;
    const char* projectID;
    const char* refreshToken;
    int authenticated;
} AppState;

void runMacGUI(AppState state);
void updateGUIState(AppState state);
void setGUIResponseText(const char* text);

#endif
