import { useResource } from '../lib/api'
import { fallbackPlans } from '../lib/fallback'
import type { Plan } from '../lib/types'

import { Header } from './Header'
import { Hero } from './Hero'
import { HowItWorks } from './HowItWorks'
import { Features } from './Features'
import { Platforms } from './Platforms'
import { Pricing } from './Pricing'
import { Limits } from './Limits'
import { Faq } from './Faq'
import { Closer } from './Closer'
import { Footer } from './Footer'

/*
  The landing page no longer reads /api/v1/status or /api/v1/locations.
  Behind this site is one server and no telemetry, so a live-looking
  network console and a table of locations could only be invented. Plans
  stay on the API: they are real numbers that change, and one place to
  change them beats three.
*/
export function Landing() {
  const plans = useResource<Plan[]>('/api/v1/plans', fallbackPlans)

  return (
    <>
      <Header />
      <main id="main" tabIndex={-1}>
        <Hero />
        <HowItWorks />
        <Features />
        <Platforms />
        <Pricing plans={plans} />
        <Limits />
        <Faq />
        <Closer />
      </main>
      <Footer />
    </>
  )
}
