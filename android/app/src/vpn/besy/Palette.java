package vpn.besy;

/**
 * The colours the design is built from, in one place.
 *
 * <p>Named for what they are in the picture rather than for where they
 * are used, because the same light serves several surfaces: the ground
 * is the night behind the glass, the flare is the source sitting behind
 * it, and the ember is the one warm note that keeps the screen from
 * going monochrome.
 */
final class Palette {
    static final int VOID_    = 0xFF050506;
    static final int DEEP     = 0xFF23394D;
    static final int HAZE     = 0xFF5F86A6;
    static final int FLARE    = 0xFFEAF4FF;
    static final int EMBER    = 0xFFC9A88B;
    static final int LIVE     = 0xFF6FE3C0;

    // the lamp under the circle
    static final int LAMP_ON   = 0xFF2FD66B;
    static final int LAMP_OFF  = 0xFFE5484D;
    static final int LAMP_WAIT = 0xFFF2B33D;

    static final int INK      = 0xFFEAF2FA;
    static final int INK_2    = 0x9EEAF2FA;
    static final int INK_3    = 0x61EAF2FA;

    static final int GLASS    = 0x11FFFFFF;
    static final int RIM      = 0x52FFFFFF;
    static final int RIM_SOFT = 0x24FFFFFF;

    private Palette() {}
}
