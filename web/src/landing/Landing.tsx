import { useResource } from '../lib/api'
import { fallbackLocations, fallbackPlans, fallbackStatus } from '../lib/fallback'
import type { Location, Plan, Status } from '../lib/types'

import { Header } from './Header'
import { Hero } from './Hero'
import { HowItWorks } from './HowItWorks'
import { Features } from './Features'
import { Modes } from './Modes'
import { Locations } from './Locations'
import { Platforms } from './Platforms'
import { Pricing } from './Pricing'
import { Limits } from './Limits'
import { Faq } from './Faq'
import { Closer } from './Closer'
import { Footer } from './Footer'

export function Landing() {
  const status = useResource<Status>('/api/v1/status', fallbackStatus)
  const locations = useResource<Location[]>('/api/v1/locations', fallbackLocations)
  const plans = useResource<Plan[]>('/api/v1/plans', fallbackPlans)

  return (
    <>
      <Header />
      <main id="main">
        <Hero status={status} />
        <HowItWorks />
        <Features />
        <Modes />
        <Locations locations={locations} sample={status.data.mock} />
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
