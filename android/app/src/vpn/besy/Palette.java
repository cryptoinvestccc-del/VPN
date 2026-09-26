package vpn.besy;

/**
 * The colours of the screens around the main one (settings, documents),
 * from the approved design's handoff: a light app on #F5F6F8 with white
 * cards, dark text, and one violet accent. The main screen keeps its own
 * colours next to the code that draws it.
 */
final class Palette {
    static final int VOID_    = 0xFFF5F6F8;   // app background
    static final int DEEP     = 0xFFF5F6F8;
    static final int INK      = 0xFF111318;   // primary text
    static final int INK_2    = 0xFF4A4E57;   // values
    static final int INK_3    = 0xFF8A8E97;   // labels, hints
    static final int LIVE     = 0xFF7C6CFF;   // accent: switches, links
    static final int EMBER    = 0xFFD0463B;   // the destructive action
    static final int SURFACE  = 0xFFFFFFFF;   // cards
    static final int RIM_SOFT = 0xFFE8EAF0;   // card borders, dividers

    // the lamp and dot colours on the main screen
    static final int LAMP_ON   = 0xFF58D68D;
    static final int LAMP_OFF  = 0xFF6F737C;
    static final int LAMP_WAIT = 0xFFF2B33D;

    private Palette() {}
}
