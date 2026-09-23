/*
  Every outbound link on the landing page in one place. The bots are the
  product: there is no checkout on this site, so a stale link here is a
  broken sale. Keeping them together means one file to edit when a bot
  moves, instead of nine components to grep.
*/

export const links = {
  /** Telegram bot: tariffs, payment and key issue. */
  telegram: 'https://t.me/besyvpnbot',
  /** The same bot in MAX, for people who do not use Telegram. */
  max: 'https://max.ru/id380806643503_1_bot',
  /** Amnezia's own documentation — we link it rather than retell it. */
  amneziaDocs: 'https://docs.amnezia.org/documentation/instructions/amnezia-on-ios-in-russia/',
  /** iOS clients that are still in the Russian App Store. */
  iosDefaultVpn: 'https://apps.apple.com/ru/app/defaultvpn/id6744725017',
  iosAmneziaWg: 'https://apps.apple.com/ru/app/amneziawg/id6478942365',
  /** Android and desktop builds come from Amnezia directly. */
  androidAmnezia: 'https://play.google.com/store/apps/details?id=org.amnezia.vpn',
  desktopAmnezia: 'https://amnezia.org/ru/downloads',
} as const
